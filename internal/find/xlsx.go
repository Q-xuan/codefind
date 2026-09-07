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
	"sort"
	"strconv"
	"strings"
)

const maxWorkbookBytes = 32 << 20
const maxWorkbookXMLBytes = 64 << 20
const maxWorkbooks = 32

var errWorkbookBudget = errors.New("workbook budget exceeded")

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
	ctx       context.Context
}

// Parts are never extracted or fetched; relationship paths only address this ZIP.
func (a *xlsxArchive) decode(name string, visit func(*xml.Decoder, xml.StartElement) error) error {
	f := a.files[name]
	if f == nil {
		return fmt.Errorf("missing XLSX part %s", name)
	}
	if f.UncompressedSize64 > uint64(a.remaining) {
		return errWorkbookBudget
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	d := xml.NewDecoder(&xlsxReader{ctx: a.ctx, r: io.LimitReader(r, a.remaining+1), remaining: &a.remaining})
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
}

func (r *xlsxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	*r.remaining -= int64(n)
	if *r.remaining < 0 {
		return n, errWorkbookBudget
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
	Expression  string `xml:",chardata" json:"expression"`
	Type        string `xml:"t,attr" json:"type,omitempty"`
	SharedIndex string `xml:"si,attr" json:"shared_index,omitempty"`
	Range       string `xml:"ref,attr" json:"range,omitempty"`
}
type xlsxCell struct {
	Ref     string           `xml:"r,attr"`
	Type    string           `xml:"t,attr"`
	Value   *string          `xml:"v"`
	Formula *FormulaEvidence `xml:"f"`
	Inline  xlsxRichText     `xml:"is"`
}
type workbookVisitor struct {
	sheet   string
	cell    func(string, xlsxCell) error
	comment func(string, string, string) error
	merge   func(string, string) error
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
	if !info.Mode().IsRegular() || info.Size() > maxWorkbookBytes {
		return errWorkbookBudget
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	a := xlsxArchive{files: map[string]*zip.File{}, remaining: maxWorkbookXMLBytes, ctx: ctx}
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
	foundSheet := visitor.sheet == ""
	for _, sheet := range sheets {
		if visitor.sheet != "" && sheet.Name != visitor.sheet {
			continue
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
			if s.Name.Local != "c" {
				return nil
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
			return err
		}
		comments, err := a.relationships(name)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(comments))
		for id := range comments {
			ids = append(ids, id)
		}
		sort.Strings(ids)
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
				return visitor.comment(sheet.Name, v.Ref, v.Text.value())
			})
			if err != nil {
				return err
			}
		}
	}
	if !foundSheet {
		return fmt.Errorf("%w: worksheet %q not found", ErrInvalidRequest, visitor.sheet)
	}
	return nil
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
