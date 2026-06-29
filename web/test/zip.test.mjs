import { describe, expect, test } from "bun:test";
import { entriesToZip, filesToZip, unzipBytes } from "../static/js/zip.js";

function fileFixture(name, body, type) {
	return new File([body], name, { lastModified: 1234, type });
}

describe("entriesToZip", () => {
	test("re-zips opened entries into a single round-trippable archive", async () => {
		const { blob } = await filesToZip([
			fileFixture("notes.txt", "hello", "text/plain"),
			fileFixture("data.bin", "payload", "application/octet-stream"),
		]);
		const entries = await unzipBytes(await blob.arrayBuffer());

		const zipBlob = await entriesToZip(entries);
		expect(zipBlob.type).toBe("application/zip");

		const reopened = await unzipBytes(await zipBlob.arrayBuffer());
		expect(reopened).toHaveLength(2);
		expect(reopened.find((e) => e.name === "notes.txt")).toBeDefined();
		expect(await reopened.find((e) => e.name === "notes.txt").blob.text()).toBe("hello");
		expect(await reopened.find((e) => e.name === "data.bin").blob.text()).toBe("payload");
	});

	test("handles empty entry list", async () => {
		const zipBlob = await entriesToZip([]);
		expect(zipBlob.type).toBe("application/zip");
		const reopened = await unzipBytes(await zipBlob.arrayBuffer());
		expect(reopened).toHaveLength(0);
	});
});
