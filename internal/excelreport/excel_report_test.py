import json
import os
import tempfile
import unittest
from unittest import mock

from internal.excelreport import excel_report


class FakeBackend:
    def __init__(self):
        self.sheet = object()
        self.cells = {
            (2, 10): "David P.",
            (4, 6): "Brown",
            (4, 7): "Alice",
        }
        self.open_readonly = None
        self.closed = False
        self.quit_called = False
        self.writes = []

    def open_workbook(self, path, readonly=True):
        self.open_readonly = readonly
        return object()

    def close_workbook(self, workbook, save_changes=False):
        self.closed = True
        self.save_changes = save_changes

    def quit(self):
        self.quit_called = True

    def get_sheet(self, workbook, name):
        if name != "Senior2":
            raise excel_report.BackendError(f"worksheet not found: {name}")
        return self.sheet

    def used_bounds(self, sheet):
        return 1, 1, 4, 15

    def get_value(self, sheet, row, col):
        return self.cells.get((row, col))

    def set_value(self, sheet, row, col, value):
        self.writes.append((row, col, value))


class PreflightReportTest(unittest.TestCase):
    def test_opens_readonly_and_does_not_write(self):
        backend = FakeBackend()
        with self.payload_file([self.student("Alice", "Brown")]) as payload_path:
            with mock.patch.object(excel_report, "find_backend", return_value=backend):
                excel_report.preflight_report("report.xls", payload_path)

        self.assertTrue(backend.open_readonly)
        self.assertTrue(backend.closed)
        self.assertFalse(backend.save_changes)
        self.assertTrue(backend.quit_called)
        self.assertEqual(backend.writes, [])

    def test_reports_all_missing_students_without_writing(self):
        backend = FakeBackend()
        rows = [
            self.student("Alice", "Brown"),
            self.student("Bob", "Zhang"),
            self.student("Carol", "Lin"),
        ]
        with self.payload_file(rows) as payload_path:
            with mock.patch.object(excel_report, "find_backend", return_value=backend):
                with self.assertRaises(excel_report.BackendError) as raised:
                    excel_report.preflight_report("report.xls", payload_path)

        self.assertIn("Bob Zhang", str(raised.exception))
        self.assertIn("Carol Lin", str(raised.exception))
        self.assertEqual(backend.writes, [])
        self.assertTrue(backend.closed)
        self.assertFalse(backend.save_changes)

    @staticmethod
    def student(first_name, last_name):
        return {"first_name": first_name, "last_name": last_name}

    @staticmethod
    def payload_file(rows, printable_path="printable.xlsx"):
        payload = {
            "sheet": "Senior2",
            "teacher": "David P.",
            "printable": printable_path,
            "rows": rows,
        }
        handle = tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", suffix=".json", delete=False)
        handle.close()
        with open(handle.name, "w", encoding="utf-8") as output:
            json.dump(payload, output)

        class Cleanup:
            def __enter__(self):
                return handle.name

            def __exit__(self, *args):
                os.remove(handle.name)

        return Cleanup()


class WriteReportFormatTest(unittest.TestCase):
    def test_xlsx_source_is_saved_as_xlsx(self):
        backend = mock.Mock()
        backend.get_sheet.return_value = object()
        backend.used_bounds.return_value = (1, 1, 4, 15)
        cells = {
            (2, 10): "David P.",
            (4, 6): "Brown",
            (4, 7): "Alice",
        }
        backend.get_value.side_effect = lambda _sheet, row, col: cells.get((row, col))

        def create_printable_copy(_doc, _sheet_name, temp_path, _new_sheet_name, format):
            self.assertEqual(format, "xlsx")
            self.assertTrue(temp_path.endswith(".xlsx"))
            with open(temp_path, "wb") as output:
                output.write(b"updated xlsx workbook")
            return object()

        backend.create_printable_copy.side_effect = create_printable_copy
        row = {
            "first_name": "Alice",
            "last_name": "Brown",
            "exam_grade": 90,
            "quarter_grade": 90,
            "quarter_letter": "A-",
        }
        with tempfile.TemporaryDirectory() as directory:
            source_path = os.path.join(directory, "report.xlsx")
            with open(source_path, "wb") as output:
                output.write(b"original workbook")
            printable_path = os.path.join(directory, "printable.xlsx")
            with PreflightReportTest.payload_file([row], printable_path) as payload_path:
                with mock.patch.object(excel_report, "find_backend", return_value=backend):
                    excel_report.write_report(source_path, payload_path)

            with open(source_path, "rb") as source:
                self.assertEqual(source.read(), b"updated xlsx workbook")

        backend.save_as.assert_called_once_with(mock.ANY, printable_path, "xlsx")

    def test_workbook_format_supports_xls_and_rejects_other_extensions(self):
        self.assertEqual(excel_report.workbook_format("report.xls"), "xls")
        self.assertEqual(excel_report.workbook_format("REPORT.XLSX"), "xlsx")
        with self.assertRaises(excel_report.BackendError):
            excel_report.workbook_format("report.ods")


if __name__ == "__main__":
    unittest.main()
