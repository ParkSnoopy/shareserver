const STORE_KEY = "shareserver.archiveList";
const ARM_KEY = "shareserver.archiveListArmed";
const SIDEBAR_EXPANDED_KEY = "shareserver.sidebarExpanded";

function navigationType() {
	return performance.getEntriesByType("navigation")[0]?.type || "";
}

function clearArchiveList() {
	sessionStorage.removeItem(STORE_KEY);
	sessionStorage.removeItem(ARM_KEY);
}

function armArchiveLinks(list) {
	for (const link of list.querySelectorAll("a.api-index-row")) {
		link.addEventListener("click", () => {
			sessionStorage.setItem(ARM_KEY, "1");
		});
	}
}

function archiveLinks(list) {
	return [...list.querySelectorAll("a.api-index-row")].map((link) => ({
		href: link.getAttribute("href") || "",
		title: link.querySelector(".archive-name")?.textContent || link.textContent || "",
		label: link.querySelector(".method-label")?.textContent || "GET",
		labelClass: link.querySelector(".method-label")?.className || "method-label method-get",
	}));
}

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

function setupSidebarToggle() {
	const toggle = document.querySelector("[data-sidebar-toggle]");
	const sidebar = document.getElementById(toggle?.getAttribute("aria-controls") || "");
	if (!toggle || !sidebar) return;
	const mobile = window.matchMedia("(max-width: 760px)");
	const setCollapsed = (collapsed, persist = false) => {
		sidebar.classList.toggle("is-collapsed", collapsed);
		toggle.setAttribute("aria-expanded", String(!collapsed));
		toggle.textContent = collapsed ? "> show archives" : "> hide archives";
		if (persist && mobile.matches) sessionStorage.setItem(SIDEBAR_EXPANDED_KEY, String(!collapsed));
	};
	toggle.addEventListener("click", () => {
		setCollapsed(!sidebar.classList.contains("is-collapsed"), true);
	});
	sidebar.addEventListener("click", (event) => {
		if (mobile.matches && event.target.closest("a.api-index-row")) setCollapsed(true, true);
	});
	const storedExpanded = sessionStorage.getItem(SIDEBAR_EXPANDED_KEY);
	setCollapsed(mobile.matches && storedExpanded !== "true");
	mobile.addEventListener("change", () => {
		if (mobile.matches) {
			setCollapsed(sessionStorage.getItem(SIDEBAR_EXPANDED_KEY) !== "true");
		} else {
			setCollapsed(false);
		}
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
	if (event.key === "F5" || ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "r")) {
		clearArchiveList();
	}
});

if (list?.dataset.privateMode === "true") {
	sessionStorage.setItem(STORE_KEY, JSON.stringify(archiveLinks(list)));
	armArchiveLinks(list);
} else if (list && location.pathname.startsWith("/s/") && navType !== "reload") {
	try {
		if (sessionStorage.getItem(ARM_KEY) === "1") {
			renderArchiveLinks(list, JSON.parse(sessionStorage.getItem(STORE_KEY) || "[]"));
		}
	} catch {
		clearArchiveList();
	}
	sessionStorage.removeItem(ARM_KEY);
}
