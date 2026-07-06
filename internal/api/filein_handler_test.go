package api

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildMultipartXLSX builds an in-memory multipart upload whose "file"
// field is an xlsx produced by the supplied row-builder. rowBuilder is
// given a *excelize.File rooted at sheet "Sheet1" so the test can lay
// out header and data rows however it likes.
func buildMultipartXLSX(t *testing.T, rowBuilder func(*excelize.File)) (*http.Request, error) {
	t.Helper()
	xf := excelize.NewFile()
	defer xf.Close()
	if rowBuilder != nil {
		rowBuilder(xf)
	}
	var buf bytes.Buffer
	if err := xf.Write(&buf); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "入库.xlsx")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, &buf); err != nil {
		return nil, fmt.Errorf("copy xlsx: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/stock/file_inbound", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}

// putRow writes row r with the supplied cell values into the default sheet.
func putRow(t *testing.T, xf *excelize.File, r int, cells ...string) {
	t.Helper()
	for c, v := range cells {
		cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
		if err := xf.SetCellValue("Sheet1", cell, v); err != nil {
			t.Fatalf("set cell %s: %v", cell, err)
		}
	}
}

// putIntRow writes row r with mixed string/int cells (real-world
// spreadsheets often store qty as a number, not a string).
func putIntRow(t *testing.T, xf *excelize.File, r int, name string, qty int) {
	t.Helper()
	nameCell, _ := excelize.CoordinatesToCellName(1, r+1)
	qtyCell, _ := excelize.CoordinatesToCellName(2, r+1)
	if err := xf.SetCellValue("Sheet1", nameCell, name); err != nil {
		t.Fatalf("set name cell: %v", err)
	}
	if err := xf.SetCellValue("Sheet1", qtyCell, qty); err != nil {
		t.Fatalf("set qty cell: %v", err)
	}
}

// runParser drives the test request through parseXlsxInbound.
var runParser = func(t *testing.T, req *http.Request) ([]fileInboundEntry, error) {
	t.Helper()
	// Limit body to 10MB to match the production handler.
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10<<20)
	return parseXlsxInbound(req)
}

func TestParseXlsxInbound_StandardTwoColumn(t *testing.T) {
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putRow(t, xf, 1, "薄荷糖支架", "5")
		putRow(t, xf, 2, "泡泡软胶支架", "30")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got=%v", len(got), got)
	}
	if got[0].name != "薄荷糖支架" || got[0].qty != 5 {
		t.Errorf("row 0 = %+v, want {薄荷糖支架 5}", got[0])
	}
	if got[1].name != "泡泡软胶支架" || got[1].qty != 30 {
		t.Errorf("row 1 = %+v, want {泡泡软胶支架 30}", got[1])
	}
}

func TestParseXlsxInbound_TrimsNameWhitespace(t *testing.T) {
	// Real 入库.xlsx has "薄荷糖支架 " with a trailing space. The
	// parser must trim so the auto-create lookup hits the existing
	// row instead of inserting a duplicate.
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putRow(t, xf, 1, "  薄荷糖支架 ", "5")
		putRow(t, xf, 2, "\t泡泡软胶支架\n", "30")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got=%v", len(got), got)
	}
	if got[0].name != "薄荷糖支架" {
		t.Errorf("row 0 name = %q, want %q", got[0].name, "薄荷糖支架")
	}
	if got[1].name != "泡泡软胶支架" {
		t.Errorf("row 1 name = %q, want %q", got[1].name, "泡泡软胶支架")
	}
}

func TestParseXlsxInbound_SkipsEmptyRows(t *testing.T) {
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putRow(t, xf, 1, "薄荷糖支架", "5")
		putRow(t, xf, 2, "", "")
		putRow(t, xf, 3, "泡泡软胶支架", "30")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got=%v", len(got), got)
	}
}

func TestParseXlsxInbound_SkipsNonNumericQty(t *testing.T) {
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putRow(t, xf, 1, "薄荷糖支架", "five")
		putRow(t, xf, 2, "泡泡软胶支架", "30")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1; got=%v", len(got), got)
	}
	if got[0].name != "泡泡软胶支架" || got[0].qty != 30 {
		t.Errorf("kept row = %+v, want {泡泡软胶支架 30}", got[0])
	}
}

func TestParseXlsxInbound_AcceptsNumericQtyCell(t *testing.T) {
	// Many real spreadsheets store qty as a number, not a string. The
	// parser must coerce either form.
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putIntRow(t, xf, 1, "薄荷糖支架", 5)
		putIntRow(t, xf, 2, "泡泡软胶支架", 30)
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got=%v", len(got), got)
	}
	if got[0].qty != 5 || got[1].qty != 30 {
		t.Errorf("qtys = %d, %d; want 5, 30", got[0].qty, got[1].qty)
	}
}

func TestParseXlsxInbound_SkipsNonPositiveQty(t *testing.T) {
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putRow(t, xf, 1, "薄荷糖支架", "0")
		putRow(t, xf, 2, "泡泡软胶支架", "-3")
		putRow(t, xf, 3, "数据线", "12")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1; got=%v", len(got), got)
	}
	if got[0].name != "数据线" {
		t.Errorf("kept name = %q, want 数据线", got[0].name)
	}
}

func TestParseXlsxInbound_AggregatesDuplicatesByName(t *testing.T) {
	// Same name across two rows should sum quantities, mirroring the
	// file_outbound behaviour so callers don't have to dedup.
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		putRow(t, xf, 0, "配件", "数量")
		putRow(t, xf, 1, "数据线", "3")
		putRow(t, xf, 2, "数据线", "5")
		putRow(t, xf, 3, "充电器", "1")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	got, err := runParser(t, req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got=%v", len(got), got)
	}
	var dataLineQty int64
	for _, e := range got {
		if e.name == "数据线" {
			dataLineQty = e.qty
		}
	}
	if dataLineQty != 8 {
		t.Errorf("数据线 qty = %d, want 8", dataLineQty)
	}
}

func TestParseXlsxInbound_RejectsMissingHeader(t *testing.T) {
	req, err := buildMultipartXLSX(t, func(xf *excelize.File) {
		// No header at row 0 — write only a data row.
		putRow(t, xf, 0, "薄荷糖支架", "5")
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	_, err = runParser(t, req)
	if err == nil {
		t.Fatal("expected error for missing header, got nil")
	}
	if !strings.Contains(err.Error(), "header") && !strings.Contains(err.Error(), "表头") {
		t.Logf("error message: %v (test passes if any error is reported)", err)
	}
}

func TestParseXlsxInbound_RejectsMissingFileField(t *testing.T) {
	// Multipart with a non-"file" field.
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("attachment", "x.xlsx")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, _ = fw.Write([]byte("not an xlsx"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/stock/file_inbound", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	_, err = runParser(t, req)
	if err == nil {
		t.Fatal("expected error for missing 'file' field, got nil")
	}
}
