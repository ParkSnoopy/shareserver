import { describe, expect, test } from "bun:test";
import {
	ArchiveError,
	ArchiveErrorCode,
	openArchive,
} from "../static/js/archive.js";
import { encryptBlob } from "../static/js/crypto.js";
import { filesToZip } from "../static/js/zip.js";

function fileFixture(name, body, type) {
	return new File([body], name, { lastModified: 1234, type });
}

async function zippedFixture(
	file = fileFixture("notes.txt", "hello", "text/plain"),
) {
	return filesToZip([file]);
}

async function expectArchiveError(promise, code) {
	try {
		await promise;
		throw new Error("openArchive unexpectedly succeeded");
	} catch (err) {
		expect(err).toBeInstanceOf(ArchiveError);
		expect(err.code).toBe(code);
		return err;
	}
}

describe("openArchive", () => {
	test("opens a plain archive with uploaded filename and type", async () => {
		const { blob, manifest } = await zippedFixture(
			fileFixture("notes.note", "hello", "application/x-note"),
		);

		const entries = await openArchive(blob, { manifest });

		expect(entries).toHaveLength(1);
		expect(entries[0].name).toBe("notes.note");
		expect(entries[0].type).toBe("application/x-note");
		expect(entries[0].previewable).toBe(false);
		expect(await entries[0].blob.text()).toBe("hello");
	});

	test("reports corrupt archives distinctly from wrong passwords", async () => {
		const err = await expectArchiveError(
			openArchive(new Blob([new Uint8Array([1, 2, 3, 4])])),
			ArchiveErrorCode.CorruptArchive,
		);

		expect(err.message).toContain("corrupt");
		expect(err.code).not.toBe(ArchiveErrorCode.WrongPassword);
	});

	test("reports missing encrypted password before decrypting", async () => {
		const { blob, manifest } = await zippedFixture();

		await expectArchiveError(
			openArchive(blob, {
				encrypted: true,
				password: "",
				cipher: {},
				manifest,
			}),
			ArchiveErrorCode.PasswordRequired,
		);
	});

	test("reports encrypted wrong password", async () => {
		const { blob, manifest } = await zippedFixture();
		const encrypted = await encryptBlob(blob, "correct horse battery staple");

		await expectArchiveError(
			openArchive(encrypted.blob, {
				encrypted: true,
				password: "wrong password",
				cipher: encrypted.meta,
				manifest,
			}),
			ArchiveErrorCode.WrongPassword,
		);
	});
});
