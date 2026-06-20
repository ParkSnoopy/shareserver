// normalizeText uses only Unicode NFC. It composes canonical Hangul Jamo
// sequences for display, but intentionally does not rewrite compatibility Jamo
// like "ㄱㅏ" into Hangul syllables.
export function normalizeText(text) {
	return (text || "").normalize("NFC");
}
