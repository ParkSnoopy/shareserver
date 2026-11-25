// Draws the village chrome with the mouse-drawing kit in /static/js/draw/:
// logo, tabs, scenery, ground, the paper sheet, page headings, button pills
// and panel frames. The real text and controls stay in the DOM.
import { onLanguageChange, translate } from "./i18n.js";

// The server renders the strings the drawings need, so the first drawing
// does not wait for the language catalog to load.
const section = document.body.dataset.section;
const preset = {
    "village.name": document.body.dataset.villageName,
    "village.footer": document.body.dataset.villageFooter,
    [`remark.${section}`]: document.body.dataset.remark,
};
const say = (key) => {
    const text = translate(key);
    return text === key ? (preset[key] ?? "") : text;
};

const BM = globalThis.BM;
const { C, cr } = BM;
const S = BM.shape;
const Ln = BM.line;
const E = BM.ell;
const V = BM.svg;
const at = (x, y, s, body) =>
    `<g transform="translate(${x} ${y}) scale(${s})">${body}</g>`;
const T = (text, x, y, size, color, outline, anchor) =>
    BM.letter(text, {
        x,
        y,
        size,
        color,
        outline: outline || null,
        anchor: anchor || "start",
    });
// The largest size up to max at which text fits into width.
const fit = (text, width, max) =>
    Math.min(max, (max * width) / Math.max(1, BM.measure(text, max)));

const mobile = matchMedia("(max-width: 760px)");

// Tab color, active label color and icon for each section.
const SECTIONS = {
    archive: [C.blue, C.sun, () => at(70, 40, 1, cr.pig("tp"))],
    upload: [C.red, "#FFF", () => at(70, 44, 1, cr.car(C.sun, "tc", 26))],
    api: [C.orange, "#FFF", () => at(70, 38, 1, cr.octopus("to"))],
    admin: [C.pink, C.sun, () => at(70, 42, 1, cr.oldman("ta"))],
    error: [C.grass, C.sun, () => at(70, 40, 1, cr.ghost("#FFF", "te"))],
};

function drawTab(tab) {
    const [color, text, icon] = SECTIONS[tab.dataset.tab];
    const on = tab.getAttribute("aria-current") === "page";
    const label = tab.querySelector(".tab-label").textContent.trim();
    const size = fit(label, 118, 26);
    // The body runs far past the bottom of the SVG, which shows overflow, so
    // a lifted tab or a low paper edge still covers the gap below the tab.
    tab.querySelector(".art").innerHTML = V(
        140,
        120,
        S(BM.tomb(4, 6, 132, 200), {
            fill: on ? color : C.off,
            sw: 5,
            seed: `tab${tab.dataset.tab}`,
        }) +
            icon() +
            T(
                label,
                70,
                98 - size,
                size,
                on ? text : "#000",
                on ? "#000" : null,
                "middle",
            ),
        "tab",
    );
}

function sun() {
    let rays = "";
    for (let i = 0; i < 10; i++) {
        const a = (i * Math.PI) / 5;
        const p = (r) => [56 + Math.cos(a) * r, 46 + Math.sin(a) * r];
        rays += Ln([p(36), p(48)], { sw: 5, stroke: C.red, seed: `ray${i}` });
    }
    return V(
        112,
        92,
        rays +
            S(E(56, 46, 30, 28), { fill: C.sun, sw: 5, seed: "sun" }) +
            BM.face(56, 46, 26, "sunf", "open"),
        "scene",
    );
}

function ground() {
    const name = say("village.name");
    return V(
        950,
        210,
        BM.poly(
            [
                [0, 210],
                [0, 70],
                [120, 52],
                [260, 66],
                [430, 46],
                [600, 62],
                [760, 44],
                [950, 58],
                [950, 210],
            ],
            { fill: C.ground, sw: 6, seed: "ground", amp: 3 },
        ) +
            T(name, 30, 78, fit(name, 260, 62), "#000") +
            at(380, 128, 1.1, cr.car(C.red, "gc", 24)) +
            at(470, 120, 1.3, cr.villager(C.blue, "gv")) +
            at(560, 128, 1.2, cr.turtle("gt")) +
            at(680, 118, 1.3, cr.pig("gp")) +
            at(800, 124, 1.2, cr.octopus("go")) +
            at(880, 110, 1, cr.flower("gf")) +
            T(say("village.footer"), 30, 166, 18, "#000"),
        "ground",
    );
}

const SCENES = {
    logo: () =>
        V(
            260,
            110,
            S(E(130, 56, 122, 46), {
                fill: C.red,
                stroke: null,
                amp: 7,
                seed: "logo",
            }) +
                T("Share://", 130, 18, 50, C.sun, "#000", "middle") +
                T(say("village.name"), 214, 70, 22, "#FFF", "#000", "middle"),
            "logo",
        ),
    sun,
    cloud: () => BM.art.scene("cloud1"),
    bird: () => BM.art.scene("bird1"),
    garbage: () => BM.art.scene("garbage"),
    ground,
};

// Decorate desktop headings; mobile keeps the server-rendered text in place.
// Long user titles stay plain text at every width.
function drawHeadings() {
    const [color, text, icon] = SECTIONS[section];
    for (const h1 of document.querySelectorAll("main h1")) {
        let art = h1.previousElementSibling;
        if (mobile.matches) {
            h1.classList.remove("sr-only");
            if (art?.classList.contains("title-art")) art.hidden = true;
            continue;
        }
        if (!art?.classList.contains("title-art")) {
            art = document.createElement("div");
            art.className = "title-art";
            art.setAttribute("aria-hidden", "true");
            h1.before(art);
        }
        const label = h1.textContent.trim();
        const drawn = [...label].length <= 24;
        h1.classList.toggle("sr-only", drawn);
        art.hidden = !drawn;
        if (!drawn) continue;
        const size = 40;
        const width = Math.ceil(BM.measure(label, size)) + 150;
        const remark =
            h1 === document.querySelector("main h1")
                ? say(`remark.${section}`)
                : "";
        const remarkSize = 16;
        const bubble = remark
            ? Math.ceil(BM.measure(remark, remarkSize)) + 40
            : 0;
        art.innerHTML = V(
            width + bubble,
            96,
            S(BM.pill(4, 8, width - 8, 80), {
                fill: color,
                sw: 6,
                seed: `h${label}`,
            }) +
                at(-6, 8, 1, icon()) +
                T(label, 120, 48 - size / 2, size, text, "#000") +
                (remark
                    ? S(E(width + bubble / 2 - 4, 46, bubble / 2 - 4, 30), {
                          fill: "#FFF",
                          sw: 4,
                          seed: `r${remark}`,
                      }) +
                      BM.poly(
                          [
                              [width + 6, 52],
                              [width - 8, 64],
                              [width + 14, 60],
                          ],
                          { fill: "#FFF", stroke: null, seed: "rt" },
                      ) +
                      T(
                          remark,
                          width + bubble / 2 - 4,
                          38,
                          remarkSize,
                          "#000",
                          null,
                          "middle",
                      )
                    : ""),
            "title",
        );
    }
}

const svgURL = (markup) =>
    `url("data:image/svg+xml,${encodeURIComponent(markup.replace("<svg ", '<svg xmlns="http://www.w3.org/2000/svg" '))}")`;

// Paint a wobbly frame as the element background. Background images survive
// i18n textContent updates, unlike child nodes.
function paint(el) {
    const w = Math.round(el.offsetWidth);
    const h = Math.round(el.offsetHeight);
    if (!w || !h || el.dataset.drawnSize === `${w}x${h}`) return;
    el.dataset.drawnSize = `${w}x${h}`;
    const seed = el.id || el.className;
    if (el.matches("button, .primary-action-button")) {
        const frame = (fill) =>
            V(
                w,
                h,
                S(
                    el.matches(".api-index-row")
                        ? BM.rrect(3, 3, w - 6, h - 6, 12)
                        : BM.pill(3, 3, w - 6, h - 6),
                    { fill, sw: 4, seed },
                ),
            );
        const off = el.matches(".language-toggle") ? C.pink : C.sky;
        el.style.setProperty("--art", svgURL(frame(off)));
        el.style.setProperty("--art-hover", svgURL(frame(C.green)));
        el.style.setProperty("--art-disabled", svgURL(frame(C.off)));
        return;
    }
    const fill = el.matches(".err")
        ? C.sun
        : el.matches(".download-notice")
          ? "#FFF7B0"
          : "#FFF";
    el.style.setProperty(
        "--art",
        svgURL(
            V(
                w,
                h,
                S(BM.rrect(4, 4, w - 8, h - 8, 18), {
                    fill,
                    sw: 5,
                    seed,
                    amp: 2.5,
                }),
            ),
        ),
    );
}

const FRAMED =
    "main button, .primary-action-button, .pill-button, .input-panel, .panel, .api-detail, .download-notice, .err, .api-sidebar, .wide";

const frames = new ResizeObserver((entries) => {
    for (const entry of entries) paint(entry.target);
});

function frameAll(root = document) {
    for (const el of root.querySelectorAll(FRAMED)) {
        if (el.classList.contains("drawn")) continue;
        el.classList.add("drawn");
        paint(el);
        frames.observe(el);
    }
}

const paper = document.querySelector(".paper");
const paperArt = document.querySelector(".paper-art");
new ResizeObserver(() => {
    const w = Math.round(paper.offsetWidth);
    const h = Math.round(paper.offsetHeight);
    paperArt.innerHTML = `<svg viewBox="0 0 ${w} ${h}" width="${w}" height="${h}" aria-hidden="true" focusable="false">${BM.paper(w, h)}</svg>`;
}).observe(paper);

// Text drawings follow the current language.
function drawText() {
    for (const tab of document.querySelectorAll(".nav-tab")) drawTab(tab);
    for (const el of document.querySelectorAll("[data-art]")) {
        el.innerHTML = SCENES[el.dataset.art]();
    }
    drawHeadings();
}

drawText();
frameAll();
// The catalog load also reports the page language; redraw only on a change.
let drawnLanguage = document.documentElement.lang;
onLanguageChange((language) => {
    if (language === drawnLanguage) return;
    drawnLanguage = language;
    drawText();
});
mobile.addEventListener("change", drawHeadings);
// Scripts add panels and buttons later (file lists, previews, results).
new MutationObserver(() => frameAll()).observe(document.querySelector("main"), {
    childList: true,
    subtree: true,
});
