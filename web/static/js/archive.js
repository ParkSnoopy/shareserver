import { decryptBlob } from "./crypto.js";
import { canPreview, mimeFromName, unzipBytes } from "./zip.js";

export const ArchiveErrorCode = Object.freeze({
	PasswordRequired: "password_required",
	WrongPassword: "wrong_password",
	CorruptArchive: "corrupt_archive",
});

export class ArchiveError extends Error {
	constructor(code, message, cause = null) {
		super(message);
		this.name = "ArchiveError";
		this.code = code;
		if (cause) this.cause = cause;
	}
}

// typeForEntry trusts manifest MIME data unless it is the generic octet fallback.
function typeForEntry(entry, manifest) {
	const manifestType =
		manifest.find((item) => item.name === entry.name)?.type || "";
	return manifestType && manifestType !== "application/octet-stream"
		? manifestType
		: mimeFromName(entry.name) || manifestType;
}

function archiveError(code, message, cause) {
	return new ArchiveError(code, message, cause);
}

// openArchive decrypts when needed, unzips the archive, and classifies entries.
export async function openArchive(blob, options = {}) {
	const {
		encrypted = false,
		password = "",
		cipher = {},
		manifest = [],
		onDecryptStart = null,
		onDecryptDone = null,
		onUnzipStart = null,
		onUnzipDone = null,
	} = options;

	let archiveBlob = blob;
	if (encrypted) {
		if (!password) {
			throw new ArchiveError(
				ArchiveErrorCode.PasswordRequired,
				"password required",
			);
		}
		onDecryptStart?.(archiveBlob);
		try {
			archiveBlob = await decryptBlob(archiveBlob, password, cipher);
		} catch (err) {
			throw archiveError(ArchiveErrorCode.WrongPassword, "wrong password", err);
		}
		onDecryptDone?.(archiveBlob);
	}

	onUnzipStart?.(archiveBlob);
	let raw;
	try {
		raw = await unzipBytes(await archiveBlob.arrayBuffer());
	} catch (err) {
		throw archiveError(ArchiveErrorCode.CorruptArchive, "corrupt archive", err);
	}
	onUnzipDone?.(archiveBlob);

	return raw.map((entry) => {
		const type = typeForEntry(entry, manifest);
		return {
			...entry,
			type,
			previewable: canPreview(entry.name, type),
		};
	});
}
