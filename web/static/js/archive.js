import { decryptBlob } from "./crypto.js";
import { translate } from "./i18n.js";
import { canPreview, mimeFromName, unzipBytes } from "./zip.js";

export const ArchiveErrorCode = Object.freeze({
	PasswordRequired: "password_required",
	WrongPassword: "wrong_password",
	UnsupportedCrypto: "unsupported_crypto",
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

function isUnsupportedCrypto(err) {
	return (
		err?.code === "unsupported_crypto" ||
		err?.name === "UnsupportedCryptoError" ||
		err?.name === "NotSupportedError"
	);
}

// openArchive decrypts when needed, unzips the archive, and classifies entries.
// Accepts a Blob or a pre-read Uint8Array; when bytes are already in memory
// (from the streaming fetch) no ArrayBuffer roundtrip is needed. When
// encrypted, the plaintext bytes flow directly into unzip without a Blob
// roundtrip — critical on memory-constrained mobile browsers where duplicated
// large buffers crash the tab.
export async function openArchive(source, options = {}) {
	const {
		encrypted = false,
		password = "",
		cipher = {},
		manifest = [],
		onDecryptStart = null,
		onDecryptDone = null,
		onDecryptDebug = null,
		onUnzipStart = null,
		onUnzipDone = null,
	} = options;

	let archiveBytes = null;
	if (encrypted) {
		if (!password) {
			throw new ArchiveError(
				ArchiveErrorCode.PasswordRequired,
				translate("archiveError.passwordRequired"),
			);
		}
		onDecryptStart?.(source);
		try {
			archiveBytes = await decryptBlob(source, password, cipher, {
				onDebug: onDecryptDebug,
				returnBytes: true,
			});
		} catch (err) {
			if (isUnsupportedCrypto(err)) {
				throw archiveError(
					ArchiveErrorCode.UnsupportedCrypto,
					err.message || translate("archiveError.unsupportedCrypto"),
					err,
				);
			}
			throw archiveError(
				ArchiveErrorCode.WrongPassword,
				translate("archiveError.wrongPassword"),
				err,
			);
		}
		onDecryptDone?.(archiveBytes);
	} else if (source instanceof Uint8Array) {
		archiveBytes = source;
	} else {
		archiveBytes = new Uint8Array(await source.arrayBuffer());
	}

	onUnzipStart?.(archiveBytes);
	let raw;
	try {
		raw = await unzipBytes(archiveBytes);
	} catch (err) {
		throw archiveError(
			ArchiveErrorCode.CorruptArchive,
			translate("archiveError.corruptArchive"),
			err,
		);
	}
	onUnzipDone?.(archiveBytes);
	archiveBytes = null;

	return raw.map((entry) => {
		const type = typeForEntry(entry, manifest);
		return {
			...entry,
			type,
			previewable: canPreview(entry.name, type),
		};
	});
}
