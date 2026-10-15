package find

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var maxWorkbookBytes int64 = 32 << 20
var maxWorkbookXMLBytes int64 = 64 << 20

const maxWorkbooks = 32

var errWorkbookBudget = errors.New("workbook budget exceeded")

// limitError is a size limit with a precise kind. Size kinds still match errWorkbookBudget.
type limitError struct {
	Kind   string
	Limit  int64
	Actual int64
	Budget int64
	Entry  string
	Sheet  string
	Sheets []SheetScan
}

func (e *limitError) Error() string {
	if e == nil {
		return "workbook budget exceeded"
	}
	if e.Entry != "" {
		return e.Kind + " " + e.Entry
	}
	return e.Kind
}

func (e *limitError) Is(target error) bool {
	if e == nil || target != errWorkbookBudget {
		return false
	}
	switch e.Kind {
	case "file_size", "xml_part", "xml_total", "sheet_limit":
		return true
	default:
		return false
	}
}

type xlsxCtxKey int

const (
	xlsxSheetListKey xlsxCtxKey = iota
	xlsxSheetFilterKey
)

func withSheetReports(ctx context.Context) (context.Context, *[]SheetScan) {
	list := &[]SheetScan{}
	return context.WithValue(ctx, xlsxSheetListKey, list), list
}

func sheetReportsFrom(ctx context.Context) []SheetScan {
	list, _ := ctx.Value(xlsxSheetListKey).(*[]SheetScan)
	if list == nil {
		return nil
	}
	return *list
}

func storeSheetReports(ctx context.Context, sheets []SheetScan) {
	list, _ := ctx.Value(xlsxSheetListKey).(*[]SheetScan)
	if list != nil {
		*list = sheets
	}
}

func withSheetFilter(ctx context.Context, names []string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	set := map[string]struct{}{}
	for _, name := range names {
		set[name] = struct{}{}
	}
	return context.WithValue(ctx, xlsxSheetFilterKey, set)
}

func sheetFilterFrom(ctx context.Context) map[string]struct{} {
	set, _ := ctx.Value(xlsxSheetFilterKey).(map[string]struct{})
	return set
}

type WorkbookLocation struct {
	Sheet  string `json:"sheet"`
	Cell   string `json:"cell"`
	Source string `json:"source"` // cell or comment
}

type xlsxRichText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (v xlsxRichText) value() string {
	s := v.Text
	for _, r := range v.Runs {
		s += r.Text
	}
	return s
}

type xlsxRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
	Type   string `xml:"Type,attr"`
	Mode   string `xml:"TargetMode,attr"`
}
type xlsxArchive struct {
	files     map[string]*zip.File
	remaining int64
	budget    int64
	part      string
	ctx       context.Context
}

// Parts are never extracted or fetched; relationship paths only address this ZIP.
func (a *xlsxArchive) decode(name string, visit func(*xml.Decoder, xml.StartElement) error) error {
	f := a.files[name]
	if f == nil {
		return fmt.Errorf("missing XLSX part %s", name)
	}
	a.part = name
	budget := a.budget
	if budget == 0 {
		budget = maxWorkbookXMLBytes
	}
	limit := a.remaining
	if limit < 0 {
		limit = 0
	}
	if f.UncompressedSize64 > uint64(limit) {
		return &limitError{Kind: "xml_part", Limit: limit, Actual: int64(f.UncompressedSize64), Budget: budget, Entry: name}
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	d := xml.NewDecoder(&xlsxReader{ctx: a.ctx, r: io.LimitReader(r, a.remaining+1), remaining: &a.remaining, budget: budget, part: name})
	for {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if start, ok := tok.(xml.StartElement); ok {
			if err := visit(d, start); err != nil {
				return err
			}
		}
	}
}

type xlsxReader struct {
	ctx       context.Context
	r         io.Reader
	remaining *int64
	budget    int64
	part      string
}

func (r *xlsxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	*r.remaining -= int64(n)
	if *r.remaining < 0 {
		over := r.budget - *r.remaining
		return n, &limitError{Kind: "xml_total", Limit: r.budget, Actual: over, Budget: r.budget, Entry: r.part}
	}
	return n, err
}
func (a *xlsxArchive) relationships(part string) (map[string]xlsxRelationship, error) {
	name := path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
	out := map[string]xlsxRelationship{}
	if a.files[name] == nil {
		return out, nil
	}
	err := a.decode(name, func(d *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local != "Relationship" {
			return nil
		}
		var v xlsxRelationship
		if err := d.DecodeElement(&v, &s); err != nil {
			return err
		}
		if v.Mode != "External" {
			out[v.ID] = v
		}
		return nil
	})
	return out, err
}
func partTarget(parent, target string) (string, error) {
	var result string
	if strings.HasPrefix(target, "/") {
		result = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		result = path.Clean(path.Join(path.Dir(parent), target))
	}
	if result == ".." || strings.HasPrefix(result, "../") || strings.ContainsAny(result, "\\:") {
		return "", errors.New("unsafe XLSX relationship")
	}
	return result, nil
}

func scanWorkbook(ctx context.Context, filename string, patterns []string, emit func(string, string, string, string) error) error {
	return walkWorkbook(ctx, filename, patterns, workbookVisitor{
		cell: func(sheet string, c xlsxCell) error {
			if c.Value == nil || *c.Value == "" {
				return nil
			}
			return emit(sheet, c.Ref, "cell", *c.Value)
		},
		comment: func(sheet, ref, text string) error { return emit(sheet, ref, "comment", text) },
	})
}

type FormulaEvidence struct {
	Expression         string `xml:",chardata" json:"expression"`
	Type               string `xml:"t,attr" json:"type,omitempty"`
	SharedIndex        string `xml:"si,attr" json:"shared_index,omitempty"`
	Range              string `xml:"ref,attr" json:"range,omitempty"`
	SharedAnchor       string `xml:"-" json:"shared_anchor,omitempty"`
	AnchorExpression   string `xml:"-" json:"anchor_expression,omitempty"`
	ResolvedExpression string `xml:"-" json:"resolved_expression,omitempty"`
	ResolveStatus      string `xml:"-" json:"resolve_status,omitempty"`
}
type xlsxCell struct {
	Ref     string           `xml:"r,attr"`
	Type    string           `xml:"t,attr"`
	Value   *string          `xml:"v"`
	Formula *FormulaEvidence `xml:"f"`
	Inline  xlsxRichText     `xml:"is"`
}
type workbookVisitor struct {
	sheet    string
	cell     func(string, xlsxCell) error
	comment  func(string, string, string) error
	merge    func(string, string) error
	shared   func(string, xlsxCell) error
	wantRow  func(int) bool
	wantCell func(string) bool
}

func xmlLocalAttr(s xml.StartElement, name string) string {
	for _, a := range s.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func walkWorkbook(ctx context.Context, filename string, patterns []string, visitor workbookVisitor) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errWorkbookBudget
	}
	if info.Size() > maxWorkbookBytes {
		return &limitError{Kind: "file_size", Limit: maxWorkbookBytes, Actual: info.Size(), Budget: maxWorkbookBytes, Entry: filepath.Base(filename)}
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	a := xlsxArchive{files: map[string]*zip.File{}, remaining: maxWorkbookXMLBytes, budget: maxWorkbookXMLBytes, ctx: ctx}
	for _, part := range z.File {
		if _, ok := a.files[part.Name]; ok {
			return errors.New("duplicate XLSX part")
		}
		a.files[part.Name] = part
	}
	shared := []string{}
	if a.files["xl/sharedStrings.xml"] != nil {
		err = a.decode("xl/sharedStrings.xml", func(d *xml.Decoder, s xml.StartElement) error {
			if s.Name.Local != "si" {
				return nil
			}
			var v xlsxRichText
			if err := d.DecodeElement(&v, &s); err != nil {
				return err
			}
			shared = append(shared, v.value())
			return nil
		})
		if err != nil {
			return err
		}
	}
	var sheets []struct {
		Name string `xml:"name,attr"`
		ID   string `xml:"id,attr"`
	}
	err = a.decode("xl/workbook.xml", func(d *xml.Decoder, s xml.StartElement) error {
		if s.Name.Local != "sheet" {
			return nil
		}
		var v struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		}
		if err := d.DecodeElement(&v, &s); err != nil {
			return err
		}
		sheets = append(sheets, v)
		return nil
	})
	if err != nil {
		return err
	}
	rels, err := a.relationships("xl/workbook.xml")
	if err != nil {
		return err
	}
	sort.SliceStable(sheets, func(i, j int) bool { return nameScore(sheets[i].Name, patterns) > nameScore(sheets[j].Name, patterns) })
	filter := sheetFilterFrom(ctx)
	foundSheet := visitor.sheet == ""
	reports := []SheetScan{}
	limited := false
	for _, sheet := range sheets {
		if visitor.sheet != "" && sheet.Name != visitor.sheet {
			continue
		}
		if len(filter) > 0 {
			if _, want := filter[sheet.Name]; !want {
				reports = append(reports, SheetScan{Name: sheet.Name, Status: "skipped", Reason: "not_requested"})
				continue
			}
		}
		rel, ok := rels[sheet.ID]
		if !ok {
			return errors.New("missing worksheet relationship")
		}
		if !strings.HasSuffix(rel.Type, "/worksheet") {
			continue
		}
		foundSheet = true
		name, err := partTarget("xl/workbook.xml", rel.Target)
		if err != nil {
			return err
		}
		report := SheetScan{Name: sheet.Name, Status: "complete", Entry: name}
		err = a.decode(name, func(d *xml.Decoder, s xml.StartElement) error {
			if s.Name.Local == "mergeCell" && visitor.merge != nil {
				var m struct {
					Ref string `xml:"ref,attr"`
				}
				if err := d.DecodeElement(&m, &s); err != nil {
					return err
				}
				return visitor.merge(sheet.Name, m.Ref)
			}
			if s.Name.Local == "row" && visitor.wantRow != nil && visitor.shared == nil {
				if r := xmlLocalAttr(s, "r"); r != "" {
					n, convErr := strconv.Atoi(r)
					if convErr == nil && !visitor.wantRow(n) {
						return d.Skip()
					}
				}
			}
			if s.Name.Local != "c" {
				return nil
			}
			if visitor.wantCell != nil {
				if ref := xmlLocalAttr(s, "r"); ref != "" && !visitor.wantCell(ref) {
					if visitor.shared == nil {
						return d.Skip()
					}
					master, ok, herr := harvestSharedMaster(d, s)
					if herr != nil {
						return herr
					}
					if ok {
						return visitor.shared(sheet.Name, master)
					}
					return nil
				}
			}
			var v xlsxCell
			if err := d.DecodeElement(&v, &s); err != nil {
				return err
			}
			switch v.Type {
			case "s":
				if v.Value == nil {
					return errors.New("missing shared string index")
				}
				i, e := strconv.Atoi(*v.Value)
				if e != nil || i < 0 || i >= len(shared) {
					return errors.New("invalid shared string index")
				}
				text := shared[i]
				v.Value = &text
			case "inlineStr":
				text := v.Inline.value()
				v.Value = &text
			}
			if !validCell(v.Ref) {
				return errors.New("invalid XLSX cell address")
			}
			return visitor.cell(sheet.Name, v)
		})
		if err != nil {
			if lim, ok := asLimit(err); ok {
				applySheetLimit(&report, lim)
				reports = append(reports, report)
				limited = true
				continue
			}
			return err
		}
		comments, err := a.relationships(name)
		if err != nil {
			if lim, ok := asLimit(err); ok {
				applySheetLimit(&report, lim)
				reports = append(reports, report)
				limited = true
				continue
			}
			return err
		}
		ids := make([]string, 0, len(comments))
		for id := range comments {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		commentFailed := false
		for _, id := range ids {
			rel := comments[id]
			if !strings.HasSuffix(rel.Type, "/comments") {
				continue
			}
			target, err := partTarget(name, rel.Target)
			if err != nil {
				return err
			}
			err = a.decode(target, func(d *xml.Decoder, s xml.StartElement) error {
				if s.Name.Local != "comment" {
					return nil
				}
				var v struct {
					Ref  string       `xml:"ref,attr"`
					Text xlsxRichText `xml:"text"`
				}
				if err := d.DecodeElement(&v, &s); err != nil {
					return err
				}
				if !validCell(v.Ref) {
					return errors.New("invalid comment address")
				}
				if visitor.wantCell != nil && !visitor.wantCell(v.Ref) {
					return nil
				}
				return visitor.comment(sheet.Name, v.Ref, v.Text.value())
			})
			if err != nil {
				if lim, ok := asLimit(err); ok {
					applySheetLimit(&report, lim)
					commentFailed = true
					break
				}
				return err
			}
		}
		reports = append(reports, report)
		if commentFailed {
			limited = true
		}
	}
	if len(filter) > 0 {
		seen := map[string]bool{}
		for _, report := range reports {
			seen[report.Name] = true
		}
		missing := []string{}
		for name := range filter {
			if !seen[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		for _, name := range missing {
			reports = append(reports, SheetScan{Name: name, Status: "partial", Reason: "sheet_not_found"})
			limited = true
		}
	}
	storeSheetReports(ctx, reports)
	if !foundSheet && visitor.sheet != "" {
		return fmt.Errorf("%w: worksheet %q not found", ErrInvalidRequest, visitor.sheet)
	}
	if limited {
		kind := "sheet_not_found"
		for _, report := range reports {
			if report.Status == "partial" && report.Reason != "sheet_not_found" {
				kind = "sheet_limit"
				break
			}
		}
		return &limitError{Kind: kind, Sheets: reports}
	}
	return nil
}

func asLimit(err error) (*limitError, bool) {
	var lim *limitError
	if errors.As(err, &lim) {
		return lim, true
	}
	return nil, false
}

func applySheetLimit(report *SheetScan, lim *limitError) {
	report.Status = "partial"
	report.Reason = lim.Kind
	report.LimitKind = lim.Kind
	report.LimitBytes = lim.Limit
	report.ActualBytes = lim.Actual
	report.BudgetBytes = lim.Budget
	if lim.Entry != "" {
		report.Entry = lim.Entry
	}
	if lim.Sheet != "" {
		report.Name = lim.Sheet
	}
}

func harvestSharedMaster(d *xml.Decoder, start xml.StartElement) (xlsxCell, bool, error) {
	cell := xlsxCell{Ref: xmlLocalAttr(start, "r")}
	depth := 1
	var formula *FormulaEvidence
	inF := false
	var buf strings.Builder
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return xlsxCell{}, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "f" && depth == 2 {
				f := &FormulaEvidence{}
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "t":
						f.Type = attr.Value
					case "si":
						f.SharedIndex = attr.Value
					case "ref":
						f.Range = attr.Value
					}
				}
				formula = f
				inF = true
				buf.Reset()
			}
		case xml.CharData:
			if inF {
				buf.Write(t)
			}
		case xml.EndElement:
			if inF && t.Name.Local == "f" {
				formula.Expression = buf.String()
				inF = false
			}
			depth--
		}
	}
	if formula == nil || formula.Type != "shared" || strings.TrimSpace(formula.Expression) == "" {
		return xlsxCell{}, false, nil
	}
	cell.Formula = formula
	return cell, true, nil
}

func validCell(ref string) bool {
	i := 0
	column := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		column = column*26 + int(ref[i]-'A') + 1
		i++
	}
	if i == 0 || i > 3 || column > 16384 || i == len(ref) || ref[i] == '0' {
		return false
	}
	for _, r := range ref[i:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, e := strconv.Atoi(ref[i:])
	return e == nil && n > 0 && n <= 1048576
}
