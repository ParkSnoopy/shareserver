import { initI18n, onLanguageChange, translate } from "./i18n.js";

await initI18n();

const STORE_KEY = "shareserver.archiveList";
const ARM_KEY = "shareserver.archiveListArmed";

// navigationType tells whether page state came from reload or navigation.
function navigationType() {
	return performance.getEntriesByType("navigation")[0]?.type || "";
}

// clearArchiveList drops cached sidebar rows so reloads show fresh server data.
function clearArchiveList() {
	sessionStorage.removeItem(STORE_KEY);
	sessionStorage.removeItem(ARM_KEY);
}

// armArchiveLinks marks archive navigation that should reuse the sidebar list.
function armArchiveLinks(list) {
	for (const link of list.querySelectorAll("a.api-index-row")) {
		link.addEventListener("click", () => {
			sessionStorage.setItem(ARM_KEY, "1");
		});
	}
}

// archiveLinks serializes visible archive rows for same-tab share navigation.
function archiveLinks(list) {
	return [...list.querySelectorAll("a.api-index-row")].map((link) => ({
		href: link.getAttribute("href") || "",
		title:
			link.querySelector(".archive-name")?.textContent ||
			link.textContent ||
			"",
		label: link.querySelector(".method-label")?.textContent || "GET",
		labelClass:
			link.querySelector(".method-label")?.className ||
			"method-label method-get",
	}));
}

// renderArchiveLinks restores cached archive rows after direct share navigation.
function renderArchiveLinks(list, links) {
	if (!links.length) return false;
	list.replaceChildren(
		...links.map((item) => {
			const link = document.createElement("a");
			link.className = "api-index-row";
			link.href = item.href;
			const title = document.createElement("span");
			title.className = "archive-name";
			title.textContent = item.title;
			const label = document.createElement("span");
			label.className = item.labelClass;
			label.textContent = item.label;
			link.append(title, label);
			return link;
		}),
	);
	armArchiveLinks(list);
	return true;
}

// setupSidebarToggle controls mobile archive visibility.
function setupSidebarToggle() {
	const toggle = document.querySelector("[data-sidebar-toggle]");
	const sidebar = document.getElementById(
		toggle?.getAttribute("aria-controls") || "",
	);
	if (!toggle || !sidebar) return;
	const layout = toggle.closest(".api-doc-layout");
	const mobile = window.matchMedia("(max-width: 760px)");
	const setCollapsed = (collapsed) => {
		sidebar.classList.toggle("is-collapsed", collapsed);
		layout?.classList.toggle(
			"is-mobile-sidebar-open",
			mobile.matches && !collapsed,
		);
		toggle.setAttribute("aria-expanded", String(!collapsed));
		toggle.textContent = collapsed
			? translate("share.showSearch")
			: translate("share.hideSearch");
	};
	toggle.addEventListener("click", () => {
		setCollapsed(!sidebar.classList.contains("is-collapsed"));
	});
	sidebar.addEventListener("click", (event) => {
		if (mobile.matches && event.target.closest("a.api-index-row"))
			setCollapsed(true);
	});
	setCollapsed(mobile.matches);
	onLanguageChange(() =>
		setCollapsed(sidebar.classList.contains("is-collapsed")),
	);
	mobile.addEventListener("change", () => {
		setCollapsed(mobile.matches);
	});
	const keyInput = sidebar.querySelector("[data-lookup-key]");
	keyInput?.addEventListener("keydown", (event) => {
		if (event.key === "Enter") {
			event.preventDefault();
			keyInput.form?.requestSubmit();
		}
	});
}

const list = document.querySelector("[data-archive-list]");
const navType = navigationType();

setupSidebarToggle();

if (navType === "reload") clearArchiveList();

document.querySelector(".logo")?.addEventListener("click", clearArchiveList);

window.addEventListener("keydown", (event) => {
	if (
		event.key === "F5" ||
		((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "r")
	) {
		clearArchiveList();
	}
});

if (list?.dataset.privateMode === "true") {
	sessionStorage.setItem(STORE_KEY, JSON.stringify(archiveLinks(list)));
	armArchiveLinks(list);
} else if (
	list &&
	location.pathname.startsWith("/s/") &&
	navType !== "reload"
) {
	try {
		if (sessionStorage.getItem(ARM_KEY) === "1") {
			renderArchiveLinks(
				list,
				JSON.parse(sessionStorage.getItem(STORE_KEY) || "[]"),
			);
		}
	} catch {
		clearArchiveList();
	}
	sessionStorage.removeItem(ARM_KEY);
}
