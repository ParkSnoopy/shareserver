(() => {
	const isAndroid = /\bAndroid\b/i.test(navigator.userAgent || "");
	const encoder = new TextEncoder();
	const maxPanelLines = 80;
	const pendingLines = [];
	const logLines = [];
	let panelBody = null;
	let copyButton = null;

	function loadDebugStyle() {
		if (document.querySelector('link[href="/dev/debug.css"]')) return;
		const link = document.createElement("link");
		link.rel = "stylesheet";
		link.href = "/dev/debug.css";
		document.head.append(link);
	}

	function ensurePanel() {
		if (panelBody || !document.body) return;
		const panel = document.createElement("section");
		panel.id = "shareserver-dev-log";
		panel.setAttribute("aria-label", "decrypt debug log");

		const actions = document.createElement("div");
		actions.className = "shareserver-dev-log-actions";

		const toggle = document.createElement("button");
		toggle.type = "button";
		toggle.textContent = "# Debug";
		toggle.addEventListener("click", () => {
			panel.classList.toggle("is-collapsed");
		});

		copyButton = document.createElement("button");
		copyButton.type = "button";
		copyButton.textContent = "copy log";
		copyButton.addEventListener("click", copyLog);

		panelBody = document.createElement("pre");
		panelBody.className = "shareserver-dev-log-body";
		actions.append(toggle, copyButton);
		panel.append(actions, panelBody);
		const progress = document.getElementById("progress");
		if (progress) progress.after(panel);
		else document.body.append(panel);

		for (const line of pendingLines.splice(0)) appendPanelLineRaw(line);
	}

	async function copyLog() {
		const text = logLines.join("\n");
		try {
			await navigator.clipboard.writeText(text);
			copyButton.textContent = "copied";
		} catch (err) {
			const area = document.createElement("textarea");
			area.value = text;
			area.setAttribute("readonly", "");
			area.style.position = "fixed";
			area.style.left = "-9999px";
			document.body.append(area);
			area.select();
			document.execCommand("copy");
			area.remove();
			copyButton.textContent = "copied";
		}
		setTimeout(() => {
			copyButton.textContent = "copy log";
		}, 1200);
	}

	function appendPanelLineRaw(line) {
		logLines.push(line);
		if (logLines.length > maxPanelLines)
			logLines.splice(0, logLines.length - maxPanelLines);
		if (!panelBody) return;
		panelBody.textContent = `${logLines.join("\n")}\n`;
		panelBody.scrollTop = panelBody.scrollHeight;
	}

	function appendPanelLine(event, detail) {
		const line = `${new Date().toISOString()} ${event} ${detail}`;
		if (!panelBody) {
			pendingLines.push(line);
			return;
		}
		appendPanelLineRaw(line);
	}

	function activeElementName() {
		const el = document.activeElement;
		if (!el) return "";
		return el.id || el.getAttribute?.("name") || el.tagName || "";
	}

	function passwordDiagnostics() {
		const input = document.getElementById("sharePassword");
		const raw = String(input?.value || "");
		const nfc = raw.normalize("NFC");
		const nfd = raw.normalize("NFD");
		return {
			inputPresent: Boolean(input),
			present: raw.length > 0,
			utf16Length: raw.length,
			codePointLength: [...raw].length,
			utf8Bytes: encoder.encode(raw).length,
			nfcChanged: nfc !== raw,
			nfdChanged: nfd !== raw,
			trimChanged: raw.trim() !== raw,
			hasCR: raw.includes("\r"),
			hasLF: raw.includes("\n"),
			hasReplacementChar: raw.includes("\uFFFD"),
			activeElement: activeElementName(),
		};
	}

	function cipherDiagnostics() {
		const root = document.getElementById("share");
		let cipher = {};
		try {
			cipher = JSON.parse(root?.dataset.cipher || "{}");
		} catch (err) {
			return { parseError: err?.message || String(err) };
		}
		return {
			kdf: cipher?.kdf || "",
			cipher: cipher?.cipher || "",
			iterations: cipher?.iterations ?? null,
			saltChars: String(cipher?.salt || "").length,
			nonceChars: String(cipher?.nonce || "").length,
		};
	}

	function pageDiagnostics() {
		const root = document.getElementById("share");
		return {
			path: location.pathname,
			android: isAndroid,
			userAgent: navigator.userAgent,
			platform: navigator.platform,
			vendor: navigator.vendor,
			secureContext: window.isSecureContext,
			subtleCrypto: Boolean(globalThis.crypto?.subtle),
			serviceWorkerController: Boolean(navigator.serviceWorker?.controller),
			shareID: root?.dataset.id || "",
			encrypted: root?.dataset.encrypted || "",
		};
	}

	function redactAndEnrich(event, data) {
		const detail = { ...pageDiagnostics(), ...data };
		if (
			event === "open-archive" ||
			event === "load-failed" ||
			event.startsWith("password-")
		) {
			detail.password = passwordDiagnostics();
		}
		if (
			event === "open-archive" ||
			event === "crypto-input" ||
			event === "load-failed"
		) {
			detail.cipher = cipherDiagnostics();
		}
		return detail;
	}

	window.shareserverDecryptDebug = (event, data = {}) => {
		let detail = "";
		try {
			detail = JSON.stringify(redactAndEnrich(event, data));
		} catch (err) {
			detail = JSON.stringify({ stringifyError: err?.message || String(err) });
		}
		console.info(`[shareserver:decrypt] ${event} ${detail}`);
		appendPanelLine(event, detail);
	};

	function boot() {
		loadDebugStyle();
		ensurePanel();
	}

	loadDebugStyle();
	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", boot, { once: true });
	} else {
		boot();
	}
})();
