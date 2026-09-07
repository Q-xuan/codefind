package find

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestEncodingValidation(t *testing.T) {
	root := makeRepo(t, map[string]string{})
	for _, tc := range []struct{ input, want string }{{"", "auto"}, {"auto", "auto"}, {"utf-8", "utf-8"}, {" GBK ", "gbk"}, {"gb18030", "gb18030"}} {
		req, err := normalizeRequest(Request{Root: root, Terms: []string{"x"}, Encoding: tc.input})
		if err != nil || req.Encoding != tc.want {
			t.Fatalf("input=%q encoding=%q err=%v", tc.input, req.Encoding, err)
		}
	}
	_, err := Find(context.Background(), Request{Root: root, Terms: []string{"x"}, Encoding: "unknown"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestEncodingChineseConfiguration(t *testing.T) {
	requireRG(t)
	for _, tc := range []struct{ encoding, content string }{
		{"auto", "id,奖励\n"}, {"utf-8", "id,奖励\n"},
		{"gbk", "id,\xbd\xb1\xc0\xf8\n"}, {"gb18030", "id,\xbd\xb1\xc0\xf8\n"},
	} {
		t.Run(tc.encoding, func(t *testing.T) {
			root := makeRepo(t, map[string]string{"awards.csv": tc.content})
			result, err := Find(context.Background(), Request{Root: root, Terms: []string{"奖励"}, Encoding: tc.encoding})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusCandidatesFound || len(result.Anchors) != 1 || result.Anchors[0].Text != "id,奖励" || result.Metrics.RGCalls != 1 {
				t.Fatalf("result=%+v", result)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Result
			if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Query.Encoding != tc.encoding {
				t.Fatalf("encoding=%q err=%v", decoded.Query.Encoding, err)
			}
		})
	}
}
