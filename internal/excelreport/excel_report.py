import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time


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


class BackendError(Exception):
    pass


class Win32Backend:
    """Microsoft Excel automation via pywin32 (Windows only)."""

    def __init__(self):
        import win32com.client as win32

        self.excel = win32.DispatchEx("Excel.Application")
        self.excel.Visible = False
        self.excel.DisplayAlerts = False

    def open_workbook(self, path, readonly=True):
        return self.excel.Workbooks.Open(
            os.path.abspath(path),
            UpdateLinks=0,
            ReadOnly=readonly,
            IgnoreReadOnlyRecommended=True,
        )

    def close_workbook(self, workbook, save_changes=False):
        workbook.Close(SaveChanges=save_changes)

    def quit(self):
        self.excel.Quit()

    def get_sheet(self, workbook, name):
        return workbook.Worksheets(name)

    def used_bounds(self, sheet):
        used = sheet.UsedRange
        first_row = used.Row
        first_col = used.Column
        last_row = first_row + used.Rows.Count - 1
        last_col = first_col + used.Columns.Count - 1
        return first_row, first_col, last_row, last_col

    def get_value(self, sheet, row, col):
        return sheet.Cells(row, col).Value

    def set_value(self, sheet, row, col, value):
        sheet.Cells(row, col).Value = value

    def save_as(self, workbook, path, format=None):
        # FileFormat numbers match the original win32 implementation:
        # 56 = Excel 97-2003 (.xls), 51 = Excel 2007+ (.xlsx)
        if format == "xls":
            workbook.SaveAs(os.path.abspath(path), FileFormat=56)
        elif format == "xlsx":
            workbook.SaveAs(os.path.abspath(path), FileFormat=51)
        else:
            workbook.SaveAs(os.path.abspath(path))

    def delete_column(self, sheet, col):
        sheet.Columns(col).Delete()

    def delete_row(self, sheet, row):
        sheet.Rows(row).Delete()

    def autofit_columns(self, sheet, cols):
        for col in cols:
            sheet.Columns(col).AutoFit()

    def set_page_setup(self, doc, sheet, landscape=True, fit_to_pages_wide=1, fit_to_pages_tall=False):
        sheet.PageSetup.Orientation = 2 if landscape else 1
        sheet.PageSetup.Zoom = False
        sheet.PageSetup.FitToPagesWide = fit_to_pages_wide
        sheet.PageSetup.FitToPagesTall = fit_to_pages_tall

    def copy_sheet_to_new_workbook(self, sheet, new_sheet_name):
        sheet.Copy()
        out = self.excel.ActiveWorkbook
        out.Worksheets(1).Name = new_sheet_name
        return out


class UnoBackend:
    """LibreOffice automation via the UNO bridge (Linux/macOS/Windows with LibreOffice)."""

    SOFFICE_BIN = shutil.which("libreoffice") or shutil.which("soffice")

    def __init__(self):
        try:
            import uno
        except ImportError as exc:
            raise BackendError(
                "LibreOffice UNO bridge not available. "
                "On Debian/Ubuntu install libreoffice-script-provider-python."
            ) from exc

        self._uno = uno
        self._port = self._start_soffice()
        self._ctx = self._connect(self._port)
        self._smgr = self._ctx.ServiceManager
        self._desktop = self._smgr.createInstanceWithContext("com.sun.star.frame.Desktop", self._ctx)

    def _start_soffice(self):
        if self.SOFFICE_BIN is None:
            raise BackendError("libreoffice / soffice not found in PATH")

        # Find an available port and start the listener there.
        for port in range(2002, 2012):
            if self._port_open(port):
                # Verify it is actually a soffice listener by connecting.
                try:
                    self._connect(port)
                    return port
                except Exception:
                    continue

            proc = subprocess.Popen(
                [
                    self.SOFFICE_BIN,
                    "--headless",
                    "--norestore",
                    "--nofirststartwizard",
                    f"--accept=socket,host=localhost,port={port};urp;StarOffice.ServiceManager",
                ],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )

            for _ in range(30):
                if proc.poll() is not None:
                    break
                if self._port_open(port):
                    return port
                time.sleep(0.2)

        raise BackendError("could not start a soffice listener on ports 2002-2011")

    @staticmethod
    def _port_open(port):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.settimeout(0.2)
            return sock.connect_ex(("127.0.0.1", port)) == 0

    def _connect(self, port):
        local_context = self._uno.getComponentContext()
        resolver = local_context.ServiceManager.createInstanceWithContext(
            "com.sun.star.bridge.UnoUrlResolver", local_context
        )
        url = (
            f"uno:socket,host=localhost,port={port};"
            "urp;StarOffice.ComponentContext"
        )
        return resolver.resolve(url)

    def _file_url(self, path):
        return self._uno.systemPathToFileUrl(os.path.abspath(path))

    def open_workbook(self, path, readonly=True):
        from com.sun.star.beans import PropertyValue

        url = self._file_url(path)
        props = (PropertyValue("Hidden", 0, True, 0),)
        if readonly:
            props += (PropertyValue("ReadOnly", 0, True, 0),)
        doc = self._desktop.loadComponentFromURL(url, "_blank", 0, props)
        if doc is None:
            raise BackendError(f"failed to open workbook: {path}")
        return doc

    def close_workbook(self, doc, save_changes=False):
        if doc is not None:
            doc.close(save_changes)

    def quit(self):
        # The soffice listener is reused if it was already running; leave it alone.
        pass

    def get_sheet(self, doc, name):
        sheets = doc.getSheets()
        if not sheets.hasByName(name):
            raise BackendError(f"worksheet not found: {name}")
        return sheets.getByName(name)

    def used_bounds(self, sheet):
        cursor = sheet.createCursor()
        cursor.gotoEndOfUsedArea(True)
        addr = cursor.getRangeAddress()
        # Convert 0-based indexes to 1-based to match win32 semantics.
        return addr.StartRow + 1, addr.StartColumn + 1, addr.EndRow + 1, addr.EndColumn + 1

    def get_value(self, sheet, row, col):
        cell = sheet.getCellByPosition(col - 1, row - 1)
        return cell.getString()

    def set_value(self, sheet, row, col, value):
        cell = sheet.getCellByPosition(col - 1, row - 1)
        if isinstance(value, bool):
            cell.setValue(int(value))
        elif isinstance(value, (int, float)):
            cell.setValue(value)
        else:
            cell.setString(str(value))

    def save_as(self, doc, path, format=None):
        from com.sun.star.beans import PropertyValue

        url = self._file_url(path)
        if format == "xls":
            filter_name = "MS Excel 97"
        elif format == "xlsx":
            filter_name = "Calc MS Excel 2007 XML"
        else:
            filter_name = None

        props = [PropertyValue("Overwrite", 0, True, 0)]
        if filter_name:
            props.append(PropertyValue("FilterName", 0, filter_name, 0))
        doc.storeToURL(url, tuple(props))

    def delete_column(self, sheet, col):
        sheet.getColumns().removeByIndex(col - 1, 1)

    def delete_row(self, sheet, row):
        sheet.getRows().removeByIndex(row - 1, 1)

    def autofit_columns(self, sheet, cols):
        for col in cols:
            sheet.getColumns().getByIndex(col - 1).setPropertyValue("OptimalWidth", True)

    def set_page_setup(self, doc, sheet, landscape=True, fit_to_pages_wide=1, fit_to_pages_tall=False):
        page_styles = doc.getStyleFamilies().getByName("PageStyles")
        page_style = page_styles.getByName(sheet.getPropertyValue("PageStyle"))
        page_style.setPropertyValue("IsLandscape", landscape)
        page_style.setPropertyValue("ScaleToPagesX", fit_to_pages_wide)
        page_style.setPropertyValue("ScaleToPagesY", int(fit_to_pages_tall) if fit_to_pages_tall else 0)

    def create_printable_copy(self, doc, sheet_name, temp_path, new_sheet_name):
        """Save the current workbook, then open that copy and trim it to one sheet."""
        self.save_as(doc, temp_path, "xls")
        self.close_workbook(doc, save_changes=False)

        printable = self.open_workbook(temp_path, readonly=False)
        sheets = printable.getSheets()
        for name in list(sheets.getElementNames()):
            if name != sheet_name:
                sheets.removeByName(name)

        sheet = sheets.getByName(sheet_name)
        sheet.setName(new_sheet_name)
        return printable


def find_backend():
    if sys.platform == "win32":
        try:
            import win32com.client  # noqa: F401
            return Win32Backend()
        except ImportError:
            pass

    # Fall back to LibreOffice UNO on any platform where it is available.
    return UnoBackend()


def find_subject_col(backend, sheet, teacher, max_col):
    target = norm(teacher)
    for col in range(1, max_col + 1):
        if norm(backend.get_value(sheet, 2, col)) == target:
            return col
    raise RuntimeError(f"teacher header not found: {teacher}")


def find_student_rows(backend, sheet, first_col, last_col, start_row, end_row):
    rows = {}
    for row in range(start_row, end_row + 1):
        first = norm(backend.get_value(sheet, row, first_col))
        last = norm(backend.get_value(sheet, row, last_col))
        if first or last:
            rows[(first, last)] = row
    return rows


def write_report(workbook_path, payload_path):
    with open(payload_path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)

    backend = find_backend()
    source_path = os.path.abspath(workbook_path)

    # Keep the temp file next to the source so os.replace stays on the same filesystem.
    fd, temp_path = tempfile.mkstemp(
        prefix="grades-excel-report-",
        suffix=".xls",
        dir=os.path.dirname(source_path),
    )
    os.close(fd)
    os.remove(temp_path)

    workbook = None
    printable = None
    try:
        workbook = backend.open_workbook(source_path, readonly=False)
        sheet = backend.get_sheet(workbook, payload["sheet"])
        _, _, last_row, last_col = backend.used_bounds(sheet)
        subject_col = find_subject_col(backend, sheet, payload["teacher"], last_col)
        row_lookup = find_student_rows(backend, sheet, 7, 6, 4, last_row)

        matched = []
        missing = []
        for item in payload["rows"]:
            key = (norm(item["first_name"]), norm(item["last_name"]))
            row = row_lookup.get(key)
            if row is None:
                missing.append(f'{item["first_name"]} {item["last_name"]}')
                continue
            backend.set_value(sheet, row, subject_col, float(item["exam_grade"]))
            backend.set_value(sheet, row, subject_col + 1, int(item["quarter_grade"]))
            backend.set_value(sheet, row, subject_col + 2, item["quarter_letter"])
            if "c_score_1" in item:
                backend.set_value(sheet, row, subject_col + 3, item["c_score_1"])
            if "c_score_2" in item:
                backend.set_value(sheet, row, subject_col + 4, item["c_score_2"])
            matched.append((row, item))

        if isinstance(backend, Win32Backend):
            printable = backend.copy_sheet_to_new_workbook(sheet, "Senior 2 APCSA")
            backend.save_as(workbook, temp_path, "xls")
            backend.close_workbook(workbook, save_changes=False)
            workbook = None
        else:
            printable = backend.create_printable_copy(workbook, payload["sheet"], temp_path, "Senior 2 APCSA")
            workbook = None

        # Replace the original workbook with the updated copy.
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

        printable_path = os.path.abspath(payload["printable"])
        os.makedirs(os.path.dirname(printable_path), exist_ok=True)
        if os.path.exists(printable_path):
            os.remove(printable_path)

        printable_sheet = backend.get_sheet(printable, "Senior 2 APCSA")
        _, _, p_last_row, p_last_col = backend.used_bounds(printable_sheet)
        keep_cols = set(range(1, 9)) | set(range(subject_col, subject_col + 5))
        for col in range(p_last_col, 0, -1):
            if col not in keep_cols:
                backend.delete_column(printable_sheet, col)

        matched_rows = {row for row, _ in matched}
        for row in range(p_last_row, 3, -1):
            if row not in matched_rows:
                backend.delete_row(printable_sheet, row)

        backend.autofit_columns(printable_sheet, keep_cols)
        backend.set_page_setup(printable, printable_sheet, landscape=True, fit_to_pages_wide=1, fit_to_pages_tall=False)
        backend.save_as(printable, printable_path, "xlsx")
        backend.close_workbook(printable, save_changes=False)
        printable = None

        print(f"Matched students: {len(matched)}")
        if missing:
            print("Missing from sheet: " + ", ".join(missing))
    finally:
        if workbook is not None:
            backend.close_workbook(workbook, save_changes=False)
        if printable is not None:
            backend.close_workbook(printable, save_changes=False)
        backend.quit()


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: excel_report.py <workbook> <payload.json>")
    write_report(sys.argv[1], sys.argv[2])
