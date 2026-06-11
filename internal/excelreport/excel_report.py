import json
import os
import sys
import tempfile

import win32com.client as win32


def norm(value):
    return " ".join(str(value or "").strip().lower().split())


def console(value):
    return str(value).encode(sys.stdout.encoding or "utf-8", errors="backslashreplace").decode(
        sys.stdout.encoding or "utf-8"
    )


def display(value):
    if value is None:
        return ""
    return str(value).strip()


def find_subject_col(ws, teacher, max_col):
    target = norm(teacher)
    for col in range(1, max_col + 1):
        if norm(ws.Cells(2, col).Value) == target:
            return col
    raise RuntimeError(f"teacher header not found: {teacher}")


def find_student_rows(ws, first_col, last_col, start_row, end_row):
    rows = {}
    for row in range(start_row, end_row + 1):
        first = norm(ws.Cells(row, first_col).Value)
        last = norm(ws.Cells(row, last_col).Value)
        if first or last:
            rows[(first, last)] = row
    return rows


def used_bounds(ws):
    used = ws.UsedRange
    first_row = used.Row
    first_col = used.Column
    last_row = first_row + used.Rows.Count - 1
    last_col = first_col + used.Columns.Count - 1
    return first_row, first_col, last_row, last_col


def write_report(workbook_path, payload_path):
    with open(payload_path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)

    excel = win32.DispatchEx("Excel.Application")
    excel.Visible = False
    excel.DisplayAlerts = False
    source_path = os.path.abspath(workbook_path)
    temp_path = None
    try:
        workbook = excel.Workbooks.Open(
            source_path,
            UpdateLinks=0,
            ReadOnly=True,
            IgnoreReadOnlyRecommended=True,
        )
        ws = workbook.Worksheets(payload["sheet"])
        _, _, last_row, last_col = used_bounds(ws)
        subject_col = find_subject_col(ws, payload["teacher"], last_col)
        row_lookup = find_student_rows(ws, 7, 6, 4, last_row)

        matched = []
        missing = []
        for item in payload["rows"]:
            key = (norm(item["first_name"]), norm(item["last_name"]))
            row = row_lookup.get(key)
            if row is None:
                missing.append(f'{item["first_name"]} {item["last_name"]}')
                continue
            ws.Cells(row, subject_col).Value = float(item["exam_grade"])
            ws.Cells(row, subject_col + 1).Value = int(item["quarter_grade"])
            ws.Cells(row, subject_col + 2).Value = item["quarter_letter"]
            if "c_score_1" in item:
                ws.Cells(row, subject_col + 3).Value = item["c_score_1"]
            if "c_score_2" in item:
                ws.Cells(row, subject_col + 4).Value = item["c_score_2"]
            matched.append((row, item))

        fd, temp_path = tempfile.mkstemp(prefix="grades-excel-report-", suffix=".xls")
        os.close(fd)
        os.remove(temp_path)
        workbook.SaveAs(temp_path, FileFormat=56)
        write_printable(excel, workbook, ws, payload, matched, subject_col)
        workbook.Close(SaveChanges=False)
    finally:
        excel.Quit()
    if temp_path:
        try:
            os.replace(temp_path, source_path)
            print("Updated workbook in place: " + console(source_path))
        except PermissionError:
            root, ext = os.path.splitext(source_path)
            updated_copy = f"{root} - updated{ext}"
            if os.path.exists(updated_copy):
                os.remove(updated_copy)
            os.replace(temp_path, updated_copy)
            print("Original workbook is locked; updated copy: " + console(updated_copy))

    print(f"Matched students: {len(matched)}")
    if missing:
        print("Missing from sheet: " + ", ".join(missing))


def write_printable(excel, source_workbook, source_ws, payload, matched, subject_col):
    printable_path = os.path.abspath(payload["printable"])
    os.makedirs(os.path.dirname(printable_path), exist_ok=True)
    if os.path.exists(printable_path):
        os.remove(printable_path)

    source_ws.Copy()
    out = excel.ActiveWorkbook
    ws = out.Worksheets(1)
    ws.Name = "Senior 2 APCSA"

    _, _, last_row, last_col = used_bounds(ws)
    keep_cols = set(range(1, 9)) | set(range(subject_col, subject_col + 5))
    for col in range(last_col, 0, -1):
        if col not in keep_cols:
            ws.Columns(col).Delete()

    matched_rows = {row for row, _ in matched}
    for row in range(last_row, 3, -1):
        if row not in matched_rows:
            ws.Rows(row).Delete()

    ws.Columns.AutoFit()
    ws.PageSetup.Orientation = 2
    ws.PageSetup.Zoom = False
    ws.PageSetup.FitToPagesWide = 1
    ws.PageSetup.FitToPagesTall = False
    out.SaveAs(printable_path, FileFormat=51)
    out.Close(SaveChanges=True)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: excel_report.py <workbook> <payload.json>")
    write_report(sys.argv[1], sys.argv[2])
