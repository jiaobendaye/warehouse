package api

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildSummaryXLSX builds a 汇总-sheet xlsx suitable for parseXlsxSummary.
// rowBuilder is given a *excelize.File rooted at sheet "汇总".
func buildSummaryXLSX(t *testing.T, rowBuilder func(*excelize.File)) (*http.Request, error) {
	t.Helper()
	xf := excelize.NewFile()
	defer xf.Close()
	// The default sheet is Sheet1; rename it to 汇总 so the parser finds it.
	if err := xf.SetSheetName("Sheet1", "汇总"); err != nil {
		return nil, fmt.Errorf("rename sheet: %w", err)
	}
	if rowBuilder != nil {
		rowBuilder(xf)
	}
	var buf bytes.Buffer
	if err := xf.Write(&buf); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "汇总.xlsx")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, &buf); err != nil {
		return nil, fmt.Errorf("copy xlsx: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/stock/file_outbound", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}

// TestParseXlsxSummary_ColumnWiseStall verifies that each column's header
// (row 0) is the stall name for entries below it, and that the same
// accessory appearing in multiple stalls aggregates quantities but takes
// its stall from the first column it appears in.
func TestParseXlsxSummary_ColumnWiseStall(t *testing.T) {
	req, err := buildSummaryXLSX(t, func(xf *excelize.File) {
		// Row 0 = stall headers; rows 1+ = data.
		putSummaryRow(t, xf, 0, "JY", "优博", "大头鸭")
		putSummaryRow(t, xf, 1, "薄荷糖支架 x2", "推拉支架 x3", "克莱因蓝 x1")
		putSummaryRow(t, xf, 2, "薄荷糖支架 x5", "", "克莱因蓝 x4")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10<<20)

	entries, err := parseXlsxSummary(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Build a lookup by name for stable assertions.
	byName := map[string]aggEntry{}
	for _, e := range entries {
		byName[e.name] = e
	}

	cases := []struct {
		name  string
		qty   int64
		stall string
	}{
		{"薄荷糖支架", 7, "JY"},   // 2 + 5, both in JY
		{"推拉支架", 3, "优博"},   // only in 优博
		{"克莱因蓝", 5, "大头鸭"}, // 1 + 4, both in 大头鸭
	}
	for _, c := range cases {
		got, ok := byName[c.name]
		if !ok {
			t.Errorf("missing %q in parsed entries", c.name)
			continue
		}
		if got.qty != c.qty {
			t.Errorf("%s qty = %d, want %d", c.name, got.qty, c.qty)
		}
		if got.stall != c.stall {
			t.Errorf("%s stall = %q, want %q", c.name, got.stall, c.stall)
		}
	}
}

// TestParseXlsxSummary_MissingColumnHeader verifies that rows beyond the
// header length default to "未分配".
func TestParseXlsxSummary_MissingColumnHeader(t *testing.T) {
	req, err := buildSummaryXLSX(t, func(xf *excelize.File) {
		putSummaryRow(t, xf, 0, "JY") // only one header column
		putSummaryRow(t, xf, 1, "薄荷糖支架 x1", "无档口配件 x2")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10<<20)

	entries, err := parseXlsxSummary(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	byName := map[string]aggEntry{}
	for _, e := range entries {
		byName[e.name] = e
	}
	if got := byName["薄荷糖支架"]; got.stall != "JY" {
		t.Errorf("薄荷糖支架 stall = %q, want JY", got.stall)
	}
	if got := byName["无档口配件"]; got.stall != "未分配" {
		t.Errorf("无档口配件 stall = %q, want 未分配 (column beyond header)", got.stall)
	}
}

// putSummaryRow writes a single row into the 汇总 sheet at 1-indexed row r+1.
func putSummaryRow(t *testing.T, xf *excelize.File, r int, cells ...string) {
	t.Helper()
	for c, v := range cells {
		cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
		if err := xf.SetCellValue("汇总", cell, v); err != nil {
			t.Fatalf("set cell %s: %v", cell, err)
		}
	}
}