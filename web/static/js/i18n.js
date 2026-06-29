const DEFAULT_LANG = "en";
const LANGS = ["en", "ko", "zh"];

let lang = DEFAULT_LANG;
let messages = {};
let ready = null;
let listeners = new Set();

function browserLang() {
	const code = (navigator.language || "").toLowerCase().split("-")[0];
	return LANGS.includes(code) ? code : DEFAULT_LANG;
}

async function loadMessages(value) {
	const chosen = LANGS.includes(value) ? value : DEFAULT_LANG;
	const res = await fetch(`/static/i18n/${chosen}.json`, { cache: "no-cache" });
	if (!res.ok) throw Error(`i18n ${chosen}`);
	messages = await res.json();
	lang = chosen;
	document.documentElement.lang = chosen;
}

function valueFor(key) {
	return key.split(".").reduce((node, part) => node?.[part], messages);
}

function fallback(key) {
	if (key === "action.download") return "> download";
	if (key === "state.done") return "done";
	if (key === "state.failed") return "failed";
	if (key.startsWith("phase.")) return key.slice("phase.".length);
	return key;
}

export function translate(key, vars = {}) {
	let text = valueFor(key);
	if (typeof text !== "string") text = fallback(key);
	return text.replace(/\{\{(\w+)\}\}/g, (_, name) => String(vars[name] ?? ""));
}

function applyNode(node) {
	let vars = {};
	if (node.dataset.i18nVars) {
		try {
			vars = JSON.parse(node.dataset.i18nVars);
		} catch {}
	}
	if (node.dataset.i18n) node.textContent = translate(node.dataset.i18n, vars);
	for (const attr of ["placeholder", "ariaLabel", "title"]) {
		const key = node.dataset[`i18n${attr[0].toUpperCase()}${attr.slice(1)}`];
		if (!key) continue;
		const real = attr === "ariaLabel" ? "aria-label" : attr;
		node.setAttribute(real, translate(key, vars));
	}
}

export function applyI18n(root = document) {
	root
		.querySelectorAll(
			"[data-i18n], [data-i18n-placeholder], [data-i18n-aria-label], [data-i18n-title]",
		)
		.forEach(applyNode);
}

export function onLanguageChange(listener) {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export async function initI18n() {
	if (!ready) {
		ready = loadMessages(browserLang())
			.catch(() => loadMessages(DEFAULT_LANG))
			.then(() => {
				applyI18n();
				for (const listener of listeners) listener(lang);
			});
	}
	return ready;
}

if (typeof document !== "undefined") initI18n();
