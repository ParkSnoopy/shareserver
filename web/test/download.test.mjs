import { describe, expect, test } from "bun:test";
import {
	contentDispositionFor,
	downloadURLPath,
	safeDownloadName,
} from "../static/js/download.js";

describe("download helpers", () => {
	test("safeDownloadName keeps uploaded basename", () => {
		expect(safeDownloadName("notes.txt")).toBe("notes.txt");
		expect(safeDownloadName("dir/report.pdf")).toBe("report.pdf");
		expect(safeDownloadName("")).toBe("download");
		expect(safeDownloadName("bad\u0000name.bin")).toBe("bad_name.bin");
	});

	test("downloadURLPath encodes token and filename", () => {
		expect(downloadURLPath("token 1", "dir/my file.bin")).toBe(
			"/__download__/token%201/my%20file.bin",
		);
	});

	test("content disposition includes ascii fallback and utf8 filename", () => {
		expect(contentDispositionFor("résumé 2026.pdf")).toBe(
			"attachment; filename=\"r_sum_ 2026.pdf\"; filename*=UTF-8''r%C3%A9sum%C3%A9%202026.pdf",
		);
	});
});
