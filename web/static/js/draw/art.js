// Every drawing on the site. Each function returns an inline <svg> string.
// Shapes come from draw.js, lettering from hangul.js.
((root) => {
    "use strict";
    const BM = root.BM;
    const {
        shape: S,
        line: Ln,
        ell: E,
        pill: P,
        rrect: R,
        tomb: Tb,
        band: Bd,
        poly: Pg,
        dot: Dt,
        face: F,
        arc: A,
        svg: V,
    } = BM;
    const C = {
        yellow: "#FFD200",
        paper: "#FFFFFF",
        red: "#FF0000",
        blue: "#005AFF",
        pink: "#FF00C0",
        orange: "#FF7E00",
        sky: "#2BB8E6",
        grass: "#42C700",
        tan: "#FF9300",
        sun: "#FFE400",
        off: "#CDCDCD",
        ground: "#BBBBBB",
        olive: "#A08A13",
        lime: "#48FF00",
        green: "#4AE73E",
        hiy: "#FFF000",
        join: "#EAFF00",
        purple: "#A35BFF",
        skin: "#F2C28B",
        brown: "#8B5A2B",
        dark: "#2E7D32",
        pig: "#F4A6C8",
        octo: "#E0605A",
    };
    BM.C = C;
    const T = (s, x, y, size, color, outline, anchor, weight) =>
        BM.letter(s, {
            x,
            y,
            size,
            color: color || "#000",
            outline: outline || null,
            anchor: anchor || "start",
            weight: weight || 11,
        });
    const at = (x, y, s, body) =>
        `<g transform="translate(${x} ${y})${s && s !== 1 ? ` scale(${s})` : ""}">${body}</g>`;
    // Two states in one drawing: CSS shows .h on hover or focus.
    const hv = (n, h) => `<g class="n">${n}</g><g class="h">${h}</g>`;

    // Union of blobs with one outline: strokes first, then all fills on top.
    function blobs(list, fill, sw, seed) {
        let out = "";
        list.forEach((p, i) => {
            out += S(p, { fill, sw: sw * 2, seed: seed + i });
        });
        list.forEach((p, i) => {
            out += S(p, { fill, stroke: null, seed: seed + i });
        });
        return out;
    }

    // ---- creatures, drawn around (0, 0) ----
    const cr = {
        fish(col, seed) {
            return (
                Pg(
                    [
                        [16, 0],
                        [30, -12],
                        [29, 12],
                    ],
                    { fill: col, seed: `${seed}t` },
                ) +
                S(E(0, 0, 20, 12), { fill: col, seed }) +
                Dt(-10, -3, 2.2) +
                S(E(-20, 3, 4, 3), { fill: "#E00", sw: 2, seed: `${seed}m` })
            );
        },
        bird(col, seed) {
            return (
                S(E(0, 0, 18, 11), { fill: col, seed }) +
                S(E(-14, -7, 8, 7), { fill: col, seed: `${seed}h` }) +
                Pg(
                    [
                        [-21, -8],
                        [-31, -4],
                        [-21, -2],
                    ],
                    { fill: C.sun, sw: 2.5, seed: `${seed}b` },
                ) +
                Dt(-15, -9, 1.8) +
                Ln(A(2, -2, 10, 7, 0.3, 2.6, 6), { sw: 2.5, seed: `${seed}w` })
            );
        },
        cloud(w, h, seed) {
            return blobs(
                [
                    E(-w * 0.25, h * 0.1, w * 0.3, h * 0.4),
                    E(0, -h * 0.15, w * 0.3, h * 0.45),
                    E(w * 0.26, h * 0.08, w * 0.27, h * 0.4),
                ],
                C.sky,
                3,
                seed,
            );
        },
        octopus(seed) {
            let g = "";
            for (let i = 0; i < 4; i++) {
                const x = -10 + i * 7;
                const leg = [
                    [x, 4],
                    [x - 3, 10],
                    [x + 2, 15],
                    [x - 2, 20],
                ];
                g +=
                    Ln(leg, { sw: 8, seed: seed + i }) +
                    Ln(leg, { sw: 4, stroke: C.octo, amp: 0, seed: seed + i });
            }
            return (
                g +
                S(E(0, -8, 14, 13), { fill: C.octo, seed }) +
                Dt(-5, -9, 1.8) +
                Dt(5, -9, 1.8)
            );
        },
        turtle(seed) {
            return (
                S(E(-24, 2, 7, 6), { fill: C.grass, seed: `${seed}h` }) +
                Dt(-26, 0, 1.4) +
                S(E(-12, 10, 5, 3), { fill: C.grass, seed: `${seed}a` }) +
                S(E(12, 10, 5, 3), { fill: C.grass, seed: `${seed}b` }) +
                Pg([...A(0, 6, 22, 18, Math.PI, Math.PI * 2, 10)], {
                    fill: C.dark,
                    seed,
                }) +
                Ln(
                    [
                        [-12, -6],
                        [12, -6],
                    ],
                    { sw: 2 },
                ) +
                Ln(
                    [
                        [0, -12],
                        [0, 6],
                    ],
                    { sw: 2 },
                )
            );
        },
        pig(seed) {
            let g = "";
            [-16, -6, 8, 18].forEach((x, i) => {
                g += Ln(
                    [
                        [x, 8],
                        [x, 20],
                    ],
                    { sw: 3, seed: seed + i },
                );
            });
            return (
                g +
                Ln(
                    [
                        [24, -2],
                        [32, -8],
                        [30, -14],
                        [26, -9],
                    ],
                    { sw: 2.5 },
                ) +
                S(E(0, 0, 24, 14), { fill: C.pig, seed }) +
                S(E(-24, -6, 12, 11), { fill: C.pig, seed: `${seed}h` }) +
                S(E(-34, -3, 5, 4), {
                    fill: C.pig,
                    sw: 2.5,
                    seed: `${seed}s`,
                }) +
                Dt(-26, -10, 1.8) +
                Pg(
                    [
                        [-22, -16],
                        [-18, -24],
                        [-14, -14],
                    ],
                    { fill: C.pig, sw: 2.5, seed: `${seed}e` },
                )
            );
        },
        car(col, seed, len) {
            const l = len || 30;
            return (
                Pg(
                    [
                        [-l, 8],
                        [-l, -4],
                        [-14, -6],
                        [-8, -18],
                        [14, -18],
                        [22, -6],
                        [l, -4],
                        [l, 8],
                    ],
                    { fill: col, seed },
                ) +
                Pg(
                    [
                        [-6, -15],
                        [10, -15],
                        [15, -7],
                        [-10, -7],
                    ],
                    { fill: C.sky, sw: 2.5, seed: `${seed}w` },
                ) +
                S(E(-l + 13, 10, 7, 7), { fill: "#666", seed: `${seed}a` }) +
                S(E(l - 13, 10, 7, 7), { fill: "#666", seed: `${seed}b` })
            );
        },
        castle(seed) {
            return (
                Ln(
                    [
                        [0, -14],
                        [0, -30],
                    ],
                    { sw: 2.5 },
                ) +
                Pg(
                    [
                        [0, -30],
                        [12, -26],
                        [0, -22],
                    ],
                    { fill: "#FFF", sw: 2, seed: `${seed}f` },
                ) +
                Pg(
                    [
                        [-30, 20],
                        [-30, 0],
                        [-20, -10],
                        [0, -14],
                        [20, -10],
                        [30, 0],
                        [30, 20],
                    ],
                    { fill: "#9A9A9A", seed },
                ) +
                Pg(
                    [
                        [-6, 20],
                        [-6, 6],
                        [6, 6],
                        [6, 20],
                    ],
                    { fill: C.brown, sw: 2.5, seed: `${seed}d` },
                )
            );
        },
        garbage(seed) {
            return (
                S(R(-14, -12, 28, 32, 3), { fill: "#6F8796", seed }) +
                S(R(-17, -18, 34, 7, 3), {
                    fill: "#8FA7B6",
                    seed: `${seed}l`,
                }) +
                Ln(
                    [
                        [-6, -6],
                        [-6, 14],
                    ],
                    { sw: 2 },
                ) +
                Ln(
                    [
                        [6, -6],
                        [6, 14],
                    ],
                    { sw: 2 },
                ) +
                Ln(
                    [
                        [-4, -22],
                        [4, -22],
                    ],
                    { sw: 3 },
                )
            );
        },
        villager(col, seed) {
            return (
                Pg(
                    [
                        [-12, 22],
                        [-10, 2],
                        [10, 2],
                        [12, 22],
                    ],
                    { fill: col, seed: `${seed}b` },
                ) +
                S(E(0, -10, 9, 11), { fill: C.skin, seed }) +
                Ln(
                    [
                        [-9, -16],
                        [-6, -22],
                        [-2, -18],
                        [2, -23],
                        [6, -18],
                        [9, -15],
                    ],
                    { sw: 3 },
                ) +
                Dt(-3, -11, 1.4) +
                Dt(4, -11, 1.4) +
                Ln(
                    [
                        [-3, -4],
                        [4, -4],
                    ],
                    { sw: 2 },
                )
            );
        },
        oldman(seed) {
            return (
                Ln(
                    [
                        [14, -2],
                        [16, 24],
                    ],
                    { sw: 3, stroke: C.brown },
                ) +
                Pg(
                    [
                        [-10, 22],
                        [-9, 0],
                        [9, 0],
                        [11, 22],
                    ],
                    { fill: "#7A4E2D", seed: `${seed}b` },
                ) +
                S(E(0, -11, 9, 10), { fill: C.skin, seed }) +
                Dt(-3, -12, 1.4) +
                Dt(4, -12, 1.4) +
                Ln(
                    [
                        [-5, -4],
                        [0, -1],
                        [5, -4],
                    ],
                    { sw: 2 },
                ) +
                Ln(
                    [
                        [11, 4],
                        [15, 2],
                    ],
                    { sw: 3 },
                )
            );
        },
        ghost(col, seed) {
            return (
                Pg(
                    [
                        [-16, 18],
                        ...A(0, -2, 16, 18, Math.PI, Math.PI * 2, 10),
                        [16, 18],
                        [8, 12],
                        [0, 18],
                        [-8, 12],
                    ],
                    { fill: col, seed },
                ) + F(0, -2, 16, `${seed}f`, "sad")
            );
        },
        heart(col, seed) {
            return Pg(
                [
                    [0, -4],
                    [-6, -10],
                    [-12, -6],
                    [-12, 2],
                    [0, 12],
                    [12, 2],
                    [12, -6],
                    [6, -10],
                ],
                { fill: col, sw: 2.5, seed },
            );
        },
        butterfly(seed) {
            return (
                S(E(-8, -2, 8, 10), { fill: C.sun, sw: 3, seed }) +
                S(E(8, -2, 8, 10), { fill: C.sun, sw: 3, seed: `${seed}r` }) +
                Ln(
                    [
                        [0, -10],
                        [0, 10],
                    ],
                    { sw: 3.5 },
                ) +
                Ln(
                    [
                        [0, -10],
                        [-4, -16],
                    ],
                    { sw: 2 },
                ) +
                Ln(
                    [
                        [0, -10],
                        [4, -16],
                    ],
                    { sw: 2 },
                )
            );
        },
        flower(seed) {
            let g =
                Ln(
                    [
                        [0, 4],
                        [1, 26],
                    ],
                    { sw: 3, stroke: "#1B7A1B" },
                ) +
                S(E(-6, 16, 5, 3), { fill: C.grass, sw: 2, seed: `${seed}l` });
            for (let i = 0; i < 6; i++) {
                const a = (i * Math.PI) / 3;
                g += S(E(Math.cos(a) * 8, -6 + Math.sin(a) * 8, 5, 5), {
                    fill: C.sun,
                    sw: 2.5,
                    seed: seed + i,
                });
            }
            return g + S(E(0, -6, 5, 5), { fill: C.brown, sw: 2.5, seed });
        },
        bottle(seed) {
            return (
                Pg(
                    [
                        [-5, -22],
                        [5, -22],
                        [5, -12],
                        [12, -4],
                        [12, 22],
                        [-12, 22],
                        [-12, -4],
                        [-5, -12],
                    ],
                    { fill: "#2E9E3E", seed },
                ) +
                S(E(0, 6, 8, 8), { fill: "#FFF", sw: 2.5, seed: `${seed}l` }) +
                T("병", 0, 0, 11, "#000", null, "middle", 12)
            );
        },
        crown(seed) {
            return (
                Pg(
                    [
                        [-26, 14],
                        [-26, -14],
                        [-14, 0],
                        [0, -18],
                        [14, 0],
                        [26, -14],
                        [26, 14],
                    ],
                    { fill: C.sky, seed },
                ) +
                Dt(-26, -16, 3.5, C.sun) +
                Dt(0, -20, 3.5, C.sun) +
                Dt(26, -16, 3.5, C.sun)
            );
        },
        redface(col, seed) {
            return (
                S(E(0, 0, 22, 21), { fill: col, seed }) +
                F(0, 0, 22, `${seed}f`, "open") +
                Ln(
                    [
                        [-12, -12],
                        [-3, -8],
                    ],
                    { sw: 2.5 },
                ) +
                Ln(
                    [
                        [12, -13],
                        [3, -8],
                    ],
                    { sw: 2.5 },
                )
            );
        },
        hand(seed) {
            return Pg(
                [
                    [-12, 20],
                    [-14, 0],
                    [-10, -4],
                    [-6, 2],
                    [-6, -18],
                    [-1, -20],
                    [2, -2],
                    [4, -14],
                    [9, -12],
                    [9, 0],
                    [14, -8],
                    [17, -4],
                    [12, 20],
                ],
                { fill: C.tan, seed },
            );
        },
    };
    BM.cr = cr;

    const art = {};

    art.logo = () =>
        V(
            250,
            122,
            S(E(125, 64, 116, 50), {
                fill: C.red,
                stroke: null,
                amp: 7,
                seed: "logo-b",
            }) +
                Ln(
                    [
                        [112, 22],
                        [104, 6],
                    ],
                    { sw: 3 },
                ) +
                Ln(
                    [
                        [124, 20],
                        [130, 4],
                    ],
                    { sw: 3 },
                ) +
                Dt(103, 5, 3.5) +
                Dt(131, 4, 3.5) +
                T("병", 10, 26, 76, C.sky, "#000") +
                T("맛", 84, 12, 70, C.sun, "#000") +
                T("골", 160, 30, 72, C.purple, "#000"),
            "logo",
        );

    const TABS = {
        comics: {
            label: "병맛만화",
            col: C.red,
            txt: "#FFF",
            icon: (on) => at(70, 40, 1, cr.redface(on ? C.sky : "#FFF", "tc")),
        },
        data: {
            label: "병맛자료",
            col: C.blue,
            txt: C.sun,
            icon: (on) => at(70, 42, 1.1, cr.ghost(on ? C.sun : "#FFF", "td")),
        },
        shop: {
            label: "병맛쇼핑",
            col: C.pink,
            txt: C.sun,
            icon: (on) =>
                Ln(A(70, 24, 11, 12, Math.PI, Math.PI * 2, 8), { sw: 3.5 }) +
                S(R(48, 22, 44, 42, 4), {
                    fill: on ? C.sun : "#FFF",
                    seed: "bag",
                }) +
                T("신세계", 70, 34, 13, "#000", null, "middle", 13),
        },
        community: {
            label: "커뮤니티",
            col: C.orange,
            txt: "#FFF",
            icon: () =>
                S(E(46, 38, 26, 20), { fill: "#FFF", seed: "cb" }) +
                Pg(
                    [
                        [40, 54],
                        [30, 66],
                        [52, 56],
                    ],
                    { fill: "#FFF", stroke: null, seed: "cbt" },
                ) +
                T("쫑알", 46, 25, 13) +
                T("쫑알", 46, 40, 13) +
                at(100, 40, 1, cr.fish(C.red, "cf")),
        },
    };
    art.tab = (key, on) => {
        const t = TABS[key];
        // The body runs past the bottom edge: the paper hides it.
        return V(
            140,
            160,
            S(Tb(4, 6, 132, 170), {
                fill: on ? t.col : C.off,
                sw: 5,
                seed: `tab${key}`,
            }) +
                t.icon(on) +
                T(
                    t.label,
                    70,
                    76,
                    28,
                    on ? t.txt : "#000",
                    on ? "#000" : null,
                    "middle",
                ),
            "tab",
        );
    };

    const rays = (cx, cy, r0, r1, a0, a1, n, seed) => {
        const p = [];
        for (let i = 0; i <= n * 2; i++) {
            const a = a0 + ((a1 - a0) * i) / (n * 2);
            const r = i % 2 ? r1 : r0;
            p.push([cx + r * Math.cos(a), cy + r * Math.sin(a)]);
        }
        p.push([cx, cy]);
        return Pg(p, { fill: C.red, seed });
    };
    art.sun = () =>
        V(
            184,
            117,
            rays(170, -6, 110, 150, Math.PI * 0.45, Math.PI * 1.05, 9, "sunr") +
                S(E(170, -6, 112, 96), { fill: C.sun, sw: 5, seed: "sun" }) +
                hv(
                    T("공지사항", 66, 22, 28, "#000", null, "start", 13),
                    Ln(
                        [
                            [94, 30],
                            [100, 32],
                        ],
                        { sw: 4 },
                    ) +
                        Ln(
                            [
                                [122, 26],
                                [128, 28],
                            ],
                            { sw: 4 },
                        ) +
                        Pg(
                            [
                                [78, 40],
                                [118, 22],
                                [122, 64],
                                [80, 52],
                            ],
                            { fill: C.red, seed: "mega" },
                        ) +
                        S(E(120, 43, 6, 21), {
                            fill: "#FFAAAA",
                            sw: 3,
                            seed: "megb",
                        }) +
                        Ln(
                            [
                                [92, 52],
                                [96, 74],
                            ],
                            { sw: 5 },
                        ),
                ),
            "sun hv",
        );

    art.loginBtn = () =>
        V(
            98,
            46,
            hv(
                S(P(3, 3, 92, 40), { fill: C.pink, seed: "lg" }) +
                    T("로그인", 49, 9, 26, C.lime, "#000", "middle"),
                S(P(3, 3, 92, 40), { fill: C.green, seed: "lg" }) +
                    T("로그인", 49, 9, 26, C.pink, "#000", "middle"),
            ),
            "hv",
        );
    art.joinBtn = () =>
        V(
            112,
            42,
            hv(
                T("회원가입", 56, 7, 26, "#FFF", "#000", "middle"),
                T("회원가입", 56, 7, 26, C.join, "#000", "middle"),
            ),
            "hv",
        );

    // Scenery. [width, height, drawing]
    const SCENE = {
        bmt: [
            76,
            56,
            () =>
                at(18, 30, 1, cr.villager(C.blue, "b1")) +
                at(58, 30, 1, cr.villager(C.pink, "g1")) +
                at(38, 10, 0.8, cr.heart(C.red, "bh")),
        ],
        flower: [34, 50, () => at(17, 20, 1, cr.flower("fl"))],
        car: [66, 40, () => at(33, 22, 1, cr.car(C.red, "car"))],
        butterfly: [36, 30, () => at(18, 16, 1, cr.butterfly("bf"))],
        fish1: [
            64,
            34,
            () =>
                at(28, 17, 1, cr.fish(C.sky, "f1")) +
                Ln(
                    [
                        [50, 14],
                        [54, 14],
                    ],
                    { sw: 2 },
                ) +
                Ln(
                    [
                        [56, 20],
                        [60, 20],
                    ],
                    { sw: 2 },
                ),
        ],
        cloud1: [100, 42, () => at(50, 22, 1, cr.cloud(84, 30, "c1"))],
        cloud2: [66, 32, () => at(33, 17, 1, cr.cloud(54, 22, "c2"))],
        bird1: [54, 34, () => at(30, 18, 1, cr.bird(C.grass, "b1"))],
        bird2: [
            70,
            42,
            () =>
                at(30, 26, 1, cr.bird(C.blue, "b2")) +
                T("NEW", 34, 2, 15, C.red, null, "start", 14),
        ],
        sungji: [66, 54, () => at(33, 32, 1, cr.castle("cs"))],
        garbage: [42, 48, () => at(21, 26, 1, cr.garbage("gb"))],
        ie6: [
            64,
            64,
            () =>
                S(E(24, 40, 16, 15), { fill: C.sky, sw: 4, seed: "ie" }) +
                T("e", 24, 26, 26, C.blue, "#000", "middle") +
                T("6", 40, 40, 18) +
                T("망", 52, 2, 14, C.red, null, "middle", 14) +
                T("해", 54, 18, 14, C.red, null, "middle", 14) +
                T("라", 52, 34, 14, C.red, null, "middle", 14),
        ],
        byung: [30, 52, () => at(15, 28, 1, cr.bottle("bt"))],
    };
    art.scene = (key) => {
        const [w, h, f] = SCENE[key];
        return V(w, h, f(), "scene");
    };

    art.ribbon = (col, items, active) =>
        V(
            570,
            72,
            S(Bd(4, 2, 562, 68, `rib${col}`), {
                fill: col,
                sw: 5,
                seed: `rb${col}`,
            }) +
                items
                    .map(
                        (it, i) =>
                            `<a href="${it.href}">` +
                            T(
                                it.label,
                                it.x,
                                22,
                                22,
                                i === active ? C.sun : "#000",
                                i === active ? "#000" : null,
                                "start",
                                13,
                            ) +
                            "</a>",
                    )
                    .join(""),
            "ribbon",
        );

    const pill = (w, h, fill, seed) =>
        S(P(4, 6, w - 8, h - 12), { fill, sw: 6, seed });
    const WIDGETS = {
        comics: () =>
            pill(377, 90, C.tan, "wc") +
            at(48, 46, 1, cr.redface(C.red, "wcf")) +
            T("새로", 84, 20, 17) +
            T("올라온", 84, 42, 17) +
            T("병맛만화", 150, 18, 52, C.sun, "#000"),
        data: () =>
            pill(377, 90, C.grass, "wd") +
            at(46, 48, 1.1, cr.ghost(C.sky, "wdg")) +
            T("새로", 84, 20, 17) +
            T("올라온", 84, 42, 17) +
            T("병맛자료", 150, 18, 52, C.sun, "#000"),
        board: () =>
            pill(377, 90, "#FFF", "wb") +
            S(E(316, 45, 44, 30), { fill: C.sky, sw: 5, seed: "wbf" }) +
            Pg(
                [
                    [272, 40],
                    [290, 46],
                    [272, 54],
                ],
                { fill: "#FFF", stroke: null, seed: "wbm" },
            ) +
            Dt(300, 36, 3) +
            T("자유게시판", 30, 22, 46, "#000", null, "start", 13),
        sungji: () =>
            pill(377, 90, C.red, "ws") +
            at(64, 52, 1.2, cr.castle("wsc")) +
            T("성지순례", 130, 20, 52, C.sun, "#000"),
    };
    art.widget = (key) => V(377, 90, WIDGETS[key](), "widget");

    const BANNERS = {
        intro: () =>
            S(R(4, 4, 222, 102, 30), { fill: C.grass, sw: 6, seed: "bi" }) +
            T("병맛골", 30, 14, 40, C.sun, "#000") +
            T("소개", 74, 56, 42, C.sky, "#000"),
        bob: () =>
            S(R(4, 4, 222, 102, 34), { fill: C.red, sw: 6, seed: "bb" }) +
            at(46, 40, 1, cr.crown("crn")) +
            T("BOB", 46, 66, 18, C.grass, null, "middle", 14) +
            T("최고", 92, 12, 32, "#FFF", "#000") +
            T("병맛만화", 88, 54, 34, C.sun, "#000"),
        pass: () =>
            S(R(4, 4, 222, 102, 30), { fill: C.sky, sw: 6, seed: "bp" }) +
            at(40, 56, 1.5, cr.bottle("bpb")) +
            T("병맛골", 84, 12, 34, C.sun, "#000") +
            T("인증소", 92, 54, 38, C.red, "#FFF"),
    };
    art.topBanner = (key) => V(230, 110, BANNERS[key](), "banner");

    art.mainBanner = () => {
        const clip =
            '<clipPath id="mb-clip"><path d="' +
            "M76 10H734Q800 10 800 76V258Q800 324 734 324H76Q10 324 10 258V76Q10 10 76 10Z" +
            '"/></clipPath>';
        const hills = Pg(
            [
                [0, 210],
                [60, 150],
                [150, 110],
                [260, 140],
                [380, 90],
                [520, 120],
                [640, 100],
                [810, 150],
                [810, 340],
                [0, 340],
            ],
            { fill: C.grass, sw: 5, seed: "mbh" },
        );
        const ground = Pg(
            [
                [0, 250],
                [200, 240],
                [420, 248],
                [620, 238],
                [810, 244],
                [810, 340],
                [0, 340],
            ],
            { fill: C.tan, sw: 5, seed: "mbg" },
        );
        const sky =
            rays(740, 40, 74, 104, Math.PI * 0.4, Math.PI * 1.15, 8, "mbr") +
            S(E(740, 40, 76, 66), { fill: C.sun, sw: 5, seed: "mbs" });
        const cast =
            at(130, 40, 1, cr.cloud(70, 26, "mc1")) +
            at(460, 46, 1, cr.cloud(60, 22, "mc2")) +
            at(220, 50, 1, cr.bird(C.grass, "mb1")) +
            Ln(
                [
                    [250, 46],
                    [254, 46],
                ],
                { sw: 2 },
            ) +
            Ln(
                [
                    [262, 46],
                    [266, 46],
                ],
                { sw: 2 },
            ) +
            at(380, 46, 1, cr.fish(C.blue, "mf")) +
            at(150, 150, 1, cr.butterfly("mbf")) +
            at(130, 210, 1, cr.flower("mfl")) +
            S(A(420, 250, 44, 34, Math.PI, Math.PI * 2, 12), {
                fill: C.blue,
                seed: "dome",
            }) +
            Ln(
                [
                    [372, 250],
                    [366, 180],
                    [380, 160],
                    [392, 166],
                ],
                { sw: 12 },
            ) +
            Ln(
                [
                    [372, 250],
                    [366, 180],
                    [380, 160],
                    [392, 166],
                ],
                { sw: 6, stroke: C.sun, amp: 0 },
            ) +
            at(560, 222, 1.3, cr.oldman("mo")) +
            at(700, 224, 1.4, cr.car(C.red, "mcar")) +
            at(80, 220, 1, cr.pig("mp")) +
            at(170, 290, 1.4, cr.octopus("mo2")) +
            at(290, 292, 1.3, cr.turtle("mt")) +
            at(480, 288, 1.2, cr.pig("mp2")) +
            at(620, 290, 1.4, cr.bird(C.brown, "mb3"));
        return V(
            810,
            334,
            "<defs>" +
                clip +
                '</defs><g clip-path="url(#mb-clip)"><rect width="810" height="334" fill="#FFF"/>' +
                hills +
                ground +
                sky +
                cast +
                "</g>" +
                T("웰컴투", 170, 120, 60, C.red, "#000") +
                T("병맛골", 360, 60, 120, C.sun, "#000") +
                S(R(10, 10, 790, 314, 66), { sw: 8, seed: "mbf", amp: 3 }),
            "mainbanner",
        );
    };

    art.shopHead = (big) =>
        V(
            big ? 816 : 760,
            110,
            S(P(4, 8, (big ? 816 : 760) - 8, 94), {
                fill: C.sky,
                sw: 6,
                seed: "sh",
            }) +
                at(48, 60, 1.6, cr.villager(C.blue, "shv")) +
                T("병맛쇼핑", 96, 26, 58, "#FFF", "#000") +
                S(E(500, 54, 66, 42), { fill: "#FFF", sw: 4, seed: "shb" }) +
                Pg(
                    [
                        [560, 60],
                        [586, 54],
                        [562, 72],
                    ],
                    { fill: "#FFF", stroke: null, seed: "shbt" },
                ) +
                (big
                    ? T("오빠~♥ 나", 500, 26, 20, "#000", null, "middle", 12) +
                      T("저거 사줘", 500, 54, 20, "#000", null, "middle", 12)
                    : T("아~", 500, 26, 20, "#000", null, "middle", 12) +
                      T("갖고싶다♡", 500, 54, 20, "#000", null, "middle", 12)) +
                at(650, 56, 2.2, cr.fish(C.pink, "shf")),
            "shophead",
        );

    art.boardTitle = () =>
        V(
            560,
            130,
            S(E(70, 32, 62, 24), { fill: C.sky, sw: 4, seed: "bt1" }) +
                T("쫑알쫑알", 70, 20, 22, C.red, null, "middle", 13) +
                T("자유 게시판", 30, 58, 64, C.sky, "#000") +
                at(470, 80, 2, cr.fish(C.red, "btf")) +
                S(E(510, 36, 8, 8), { fill: "#FFF", sw: 3, seed: "bb1" }) +
                S(E(528, 18, 5, 5), { fill: "#FFF", sw: 3, seed: "bb2" }),
            "title",
        );

    // Header band of a board list. The labels sit a few pixels off on purpose.
    art.boardHead = (w) =>
        V(
            w,
            83,
            S(R(6, 10, w - 12, 64, 32), {
                fill: C.yellow,
                sw: 6,
                seed: "bh",
                amp: 3,
            }) +
                T("번호", 36, 30, 20, "#000", null, "middle", 13) +
                T("제목", w * 0.38, 26, 22, "#000", null, "middle", 13) +
                T("글쓴날", w - 188, 32, 19, "#000", null, "middle", 13) +
                T("글쓴주민", w - 102, 24, 17, "#000", null, "middle", 13) +
                T("조회수", w - 32, 31, 17, "#000", null, "middle", 13),
            "boardhead",
        );

    art.notice = () =>
        V(
            44,
            26,
            S(P(2, 3, 40, 20), { fill: C.red, sw: 3, seed: "nt" }) +
                T("공지", 22, 5, 15, "#FFF", null, "middle", 13),
            "mark",
        );

    const BTN = {
        write: [
            110,
            56,
            () =>
                hv(
                    S(P(4, 5, 102, 46), { fill: C.sky, sw: 5, seed: "bw" }) +
                        T("글쓰기", 55, 14, 28, "#FFF", "#000", "middle"),
                    S(P(4, 5, 102, 46), { fill: C.green, sw: 5, seed: "bw" }) +
                        T("글쓰기", 55, 14, 28, C.sun, "#000", "middle"),
                ),
        ],
        list: [
            100,
            50,
            () =>
                hv(
                    S(P(4, 5, 92, 40), { fill: "#000", sw: 4, seed: "bl" }) +
                        T("목록", 50, 12, 26, "#FFF", null, "middle", 13),
                    S(P(4, 5, 92, 40), { fill: "#333", sw: 4, seed: "bl" }) +
                        T("목록", 50, 12, 26, C.sun, null, "middle", 13),
                ),
        ],
        go: [
            120,
            40,
            () =>
                hv(
                    S(P(3, 3, 114, 34), { fill: C.red, sw: 3, seed: "bg" }) +
                        T("보러가기 →", 60, 10, 18, "#FFF", null, "middle", 13),
                    S(P(3, 3, 114, 34), { fill: C.red, sw: 3, seed: "bg" }) +
                        T("보러가기 →", 60, 10, 18, C.sun, null, "middle", 13),
                ),
        ],
        view: [
            110,
            34,
            () =>
                hv(
                    S(R(3, 3, 104, 28, 6), { fill: C.red, sw: 3, seed: "bv" }) +
                        T("인터뷰 보기", 55, 9, 16, "#FFF", null, "middle", 13),
                    S(R(3, 3, 104, 28, 6), { fill: C.red, sw: 3, seed: "bv" }) +
                        T("인터뷰 보기", 55, 9, 16, C.sun, null, "middle", 13),
                ),
        ],
        buy: [
            120,
            46,
            () =>
                hv(
                    S(P(3, 4, 114, 38), { fill: C.red, sw: 4, seed: "bu" }) +
                        T("주문하기", 60, 12, 22, "#FFF", null, "middle", 13),
                    S(P(3, 4, 114, 38), { fill: C.green, sw: 4, seed: "bu" }) +
                        T("주문하기", 60, 12, 22, C.sun, "#000", "middle"),
                ),
        ],
        cart: [
            140,
            46,
            () =>
                hv(
                    S(P(3, 4, 134, 38), { fill: C.sky, sw: 4, seed: "bc" }) +
                        T(
                            "장바구니담기",
                            70,
                            13,
                            19,
                            "#000",
                            null,
                            "middle",
                            13,
                        ),
                    S(P(3, 4, 134, 38), { fill: C.green, sw: 4, seed: "bc" }) +
                        T("장바구니담기", 70, 13, 19, C.sun, "#000", "middle"),
                ),
        ],
        wish: [
            140,
            46,
            () =>
                hv(
                    S(P(3, 4, 134, 38), { fill: "#FFF", sw: 4, seed: "bwi" }) +
                        T(
                            "관심물품등록",
                            70,
                            13,
                            19,
                            "#000",
                            null,
                            "middle",
                            13,
                        ),
                    S(P(3, 4, 134, 38), { fill: C.green, sw: 4, seed: "bwi" }) +
                        T("관심물품등록", 70, 13, 19, C.sun, "#000", "middle"),
                ),
        ],
        products: [
            120,
            46,
            () =>
                hv(
                    S(P(3, 4, 114, 38), { fill: "#000", sw: 4, seed: "bpl" }) +
                        T("상품 목록", 60, 12, 21, "#FFF", null, "middle", 13),
                    S(P(3, 4, 114, 38), { fill: "#333", sw: 4, seed: "bpl" }) +
                        T("상품 목록", 60, 12, 21, C.sun, null, "middle", 13),
                ),
        ],
    };
    art.btn = (key) => {
        const [w, h, f] = BTN[key];
        return V(w, h, f(), "hv");
    };

    art.titleBar = () =>
        V(
            778,
            81,
            S(R(6, 8, 766, 66, 33), {
                fill: C.yellow,
                sw: 6,
                seed: "tb",
                amp: 3,
            }) +
                Pg(
                    [
                        [10, 30],
                        [24, 12],
                        [110, 10],
                        [104, 72],
                        [22, 70],
                        [10, 52],
                    ],
                    { fill: "#000", seed: "tbs" },
                ) +
                T("제목", 58, 28, 24, "#FFF", null, "middle", 13),
            "titlebar",
        );

    art.infoBand = () =>
        V(
            790,
            100,
            Ln(
                [
                    [4, 94],
                    [200, 90],
                    [420, 96],
                    [600, 90],
                    [786, 94],
                ],
                { sw: 5, seed: "ib" },
            ) +
                T("작", 14, 14, 18) +
                T("성", 14, 36, 18) +
                T("자", 14, 58, 18) +
                at(64, 62, 1.5, cr.villager(C.blue, "ibv")) +
                S(E(210, 50, 92, 32), { fill: C.red, sw: 4, seed: "ibr" }) +
                Pg(
                    [
                        [118, 54],
                        [100, 62],
                        [122, 66],
                    ],
                    { fill: C.red, sw: 3, seed: "ibt" },
                ) +
                T("추천", 414, 4, 20) +
                at(470, 64, 1.4, cr.bird(C.grass, "ibb")) +
                S(E(626, 50, 96, 32), { fill: "#FFF", sw: 4, seed: "ibw" }),
            "infoband",
        );

    // Login board pieces.
    art.loginTitle = () =>
        V(
            250,
            96,
            S(E(92, 50, 88, 40), { fill: "#000", seed: "lt" }) +
                T("로그인", 92, 30, 42, C.sun, null, "middle", 13) +
                at(212, 56, 1.2, cr.bird(C.olive, "ltb")),
        );
    art.loginBoard = () =>
        V(
            465,
            446,
            S(R(6, 6, 453, 434, 40), {
                fill: C.olive,
                sw: 6,
                seed: "lb",
                amp: 3,
            }) +
                Ln(
                    [
                        [10, 300],
                        [160, 296],
                        [300, 304],
                        [455, 298],
                    ],
                    { sw: 5, seed: "lbl" },
                ),
        );
    art.field = () =>
        V(
            283,
            83,
            S(E(141, 41, 134, 34), { fill: "#FFF", sw: 5, seed: "fld" }),
        );
    art.fieldLabel = (s) => V(130, 44, T(s, 4, 8, 28, C.sun, "#000"));
    art.autoLogin = () => V(130, 30, T("자동로그인", 2, 4, 21, "#FFF", "#000"));
    art.enter = () =>
        V(
            130,
            74,
            hv(
                at(26, 46, 1.4, cr.villager(C.blue, "en")) +
                    S(E(84, 36, 42, 26), { fill: C.red, sw: 4, seed: "ens" }) +
                    T("입장", 84, 22, 28, C.sun, "#000", "middle"),
                at(26, 46, 1.4, cr.villager(C.blue, "en")) +
                    S(E(84, 36, 42, 26), {
                        fill: C.green,
                        sw: 4,
                        seed: "ens",
                    }) +
                    T("입장", 84, 22, 28, C.red, "#000", "middle"),
            ),
            "hv",
        );
    art.loginJoin = () =>
        V(
            170,
            54,
            hv(
                at(22, 30, 1, cr.octopus("lj")) +
                    S(P(44, 8, 120, 40), { fill: C.sky, sw: 4, seed: "ljp" }) +
                    T("회원가입", 104, 16, 22, "#000", null, "middle", 13),
                at(22, 30, 1, cr.octopus("lj")) +
                    S(P(44, 8, 120, 40), {
                        fill: C.green,
                        sw: 4,
                        seed: "ljp",
                    }) +
                    T("회원가입", 104, 16, 22, C.sun, "#000", "middle"),
            ),
            "hv",
        );
    art.loginFind = () =>
        V(
            196,
            54,
            hv(
                S(P(4, 8, 124, 40), { fill: C.grass, sw: 4, seed: "lf" }) +
                    T("정보 찾기", 66, 16, 22, "#000", null, "middle", 13) +
                    at(162, 34, 1, cr.turtle("lft")),
                S(P(4, 8, 124, 40), { fill: C.lime, sw: 4, seed: "lf" }) +
                    T("정보 찾기", 66, 16, 22, C.red, null, "middle", 13) +
                    at(162, 34, 1, cr.turtle("lft")),
            ),
            "hv",
        );
    art.close = () =>
        V(
            48,
            56,
            S(E(24, 22, 20, 19), { fill: "#000", seed: "cl" }) +
                Ln(
                    [
                        [16, 14],
                        [32, 30],
                    ],
                    { sw: 4, stroke: "#FFF" },
                ) +
                Ln(
                    [
                        [32, 14],
                        [16, 30],
                    ],
                    { sw: 4, stroke: "#FFF" },
                ) +
                T("닫기", 24, 42, 13, "#000", null, "middle", 13),
        );

    art.footer = () =>
        V(
            950,
            285,
            Pg(
                [
                    [0, 285],
                    [0, 100],
                    [90, 80],
                    [240, 92],
                    [400, 74],
                    [560, 86],
                    [720, 70],
                    [860, 84],
                    [950, 76],
                    [950, 285],
                ],
                { fill: C.ground, sw: 6, seed: "ft", amp: 3 },
            ) +
                T("병맛", 26, 112, 78, "#000") +
                T("닷컴", 180, 140, 42, "#000") +
                at(290, 196, 1, cr.car(C.red, "fc", 22)) +
                at(350, 190, 1.2, cr.octopus("fo")) +
                at(420, 196, 1.2, cr.bird(C.olive, "fb")) +
                at(480, 184, 1.3, cr.villager(C.blue, "fv")) +
                S(A(560, 214, 40, 30, Math.PI, Math.PI * 2, 10), {
                    fill: C.blue,
                    seed: "fd",
                }) +
                Ln(
                    [
                        [528, 210],
                        [520, 150],
                        [532, 134],
                        [544, 138],
                    ],
                    { sw: 10 },
                ) +
                Ln(
                    [
                        [528, 210],
                        [520, 150],
                        [532, 134],
                        [544, 138],
                    ],
                    { sw: 5, stroke: C.sun, amp: 0 },
                ) +
                at(680, 200, 1.4, cr.turtle("ft2")) +
                at(830, 170, 1.4, cr.pig("fp")) +
                Ln(
                    [
                        [740, 220],
                        [790, 196],
                        [830, 206],
                        [880, 186],
                        [920, 196],
                    ],
                    { sw: 10, stroke: C.orange },
                ) +
                T(
                    "COPYRIGHT (C) 2011 병맛골 All Rights reserved.",
                    190,
                    238,
                    22,
                    "#000",
                    null,
                    "start",
                    10,
                ),
            "footer",
        );
    art.footLink = (s) => {
        const w = Math.ceil(BM.measure(s, 20)) + 10;
        return V(
            w,
            32,
            hv(
                T(s, 5, 6, 20, "#000", null, "start", 12),
                T(s, 5, 6, 20, C.blue, null, "start", 12),
            ),
            "hv",
        );
    };

    art.remote = (up) =>
        V(
            34,
            34,
            hv(
                S(E(17, 17, 14, 14), { fill: C.red, sw: 3, seed: `rm${up}` }) +
                    Ln(
                        up
                            ? [
                                  [17, 25],
                                  [17, 9],
                              ]
                            : [
                                  [17, 9],
                                  [17, 25],
                              ],
                        { sw: 3.5, stroke: "#FFF", amp: 0 },
                    ) +
                    Ln(
                        up
                            ? [
                                  [10, 15],
                                  [17, 8],
                                  [24, 15],
                              ]
                            : [
                                  [10, 19],
                                  [17, 26],
                                  [24, 19],
                              ],
                        { sw: 3.5, stroke: "#FFF", amp: 0 },
                    ),
                S(E(17, 17, 14, 14), {
                    fill: C.green,
                    sw: 3,
                    seed: `rm${up}`,
                }) +
                    Ln(
                        up
                            ? [
                                  [17, 25],
                                  [17, 9],
                              ]
                            : [
                                  [17, 9],
                                  [17, 25],
                              ],
                        { sw: 3.5, stroke: "#FFF", amp: 0 },
                    ) +
                    Ln(
                        up
                            ? [
                                  [10, 15],
                                  [17, 8],
                                  [24, 15],
                              ]
                            : [
                                  [10, 19],
                                  [17, 26],
                                  [24, 19],
                              ],
                        { sw: 3.5, stroke: "#FFF", amp: 0 },
                    ),
            ),
            "hv",
        );

    // Comics landing.
    art.comicsLogo = () =>
        V(
            229,
            142,
            [E(80, 50, 60, 40), E(150, 70, 60, 40), E(110, 100, 70, 30)]
                .map((p, i) =>
                    S(p, { fill: C.red, stroke: null, amp: 5, seed: `cl${i}` }),
                )
                .join("") +
                T("병맛", 10, 6, 62, C.sun, "#000") +
                T("만화", 96, 64, 64, C.sky, "#000") +
                at(52, 108, 1, cr.redface(C.sky, "clf")),
        );
    art.comicsBanner1 = () =>
        V(
            200,
            100,
            S(E(100, 50, 92, 44), { fill: C.grass, sw: 6, seed: "cb1" }),
        );
    art.comicsBanner2 = () =>
        V(
            200,
            120,
            hv(
                S(E(100, 60, 94, 54), { fill: C.sky, sw: 6, seed: "cb2" }) +
                    S(R(34, 24, 132, 34, 4), {
                        fill: C.red,
                        sw: 4,
                        seed: "cbt",
                    }) +
                    T("나도", 46, 30, 22, C.sun, null, "start", 13) +
                    T("올리기", 94, 30, 22, "#FFF", null, "start", 13) +
                    at(86, 86, 1.1, cr.hand("cbh")),
                S(E(100, 60, 94, 54), { fill: C.green, sw: 6, seed: "cb2" }) +
                    S(R(34, 24, 132, 34, 4), {
                        fill: C.red,
                        sw: 4,
                        seed: "cbt",
                    }) +
                    T("나도", 46, 30, 22, C.sun, null, "start", 13) +
                    T("올리기", 94, 30, 22, "#FFF", null, "start", 13) +
                    at(86, 80, 1.1, cr.hand("cbh")),
            ),
            "hv",
        );
    const STITLE = {
        new: [
            C.sky,
            () => at(60, 46, 1.3, cr.bird(C.brown, "stn")),
            [
                ["신규등록", C.grass],
                [" 만화", C.sun],
            ],
        ],
        popular: [
            C.tan,
            () => at(52, 50, 1.4, cr.octopus("stp")),
            [
                ["인기", C.sun],
                [" 병맛만화", C.sun],
            ],
        ],
        interview: [
            C.grass,
            () => "",
            [
                ["작가", "#FFF"],
                [" 인터뷰", C.sky],
            ],
        ],
        author: [
            C.pink,
            () => at(70, 54, 1.4, cr.villager(C.blue, "sta")),
            [
                ["작가별", "#FFF"],
                [" 보기", "#FFF"],
            ],
        ],
    };
    art.sectionTitle = (key) => {
        const [fill, ch, parts] = STITLE[key];
        let x = key === "interview" ? 60 : 112;
        let txt = "";
        parts.forEach(([s, col]) => {
            txt += T(s, x, 26, 38, col, "#000");
            x += BM.measure(s, 38);
        });
        return V(
            390,
            90,
            pill(390, 90, fill, `st${key}`) + ch() + txt,
            "widget",
        );
    };
    art.comicsTab = (label, col, on) =>
        V(
            180,
            47,
            S(R(4, 6, 172, 44, 14), {
                fill: on ? "#FFF" : "#EEE",
                sw: 4,
                seed: `ct${label}`,
            }) + T(label, 90, 14, 22, on ? col : "#888", null, "middle", 13),
        );

    // Placeholder doodle where the original has a raster thumbnail.
    art.thumb = (seed, w, h) =>
        V(
            w,
            h,
            S(R(4, 4, w - 8, h - 8, 10), {
                fill: "#FFF",
                sw: 4,
                seed: `th${seed}`,
            }) +
                at(
                    w / 2,
                    h / 2 + 4,
                    Math.min(w, h) / 70,
                    cr.redface(
                        [C.sky, C.sun, C.pig, C.grass][
                            BM.hash(String(seed)) % 4
                        ],
                        `thf${seed}`,
                    ),
                ),
        );

    // Shop product doodles in a 140 x 110 box.
    const PROD = {
        duck: () =>
            S(R(18, 10, 104, 90, 4), {
                fill: "#FFF",
                sw: 6,
                stroke: C.brown,
                seed: "dk",
            }) +
            S(E(74, 64, 26, 16), { fill: C.sun, sw: 3, seed: "dkb" }) +
            S(E(56, 44, 12, 11), { fill: C.sun, sw: 3, seed: "dkh" }) +
            Pg(
                [
                    [46, 44],
                    [36, 48],
                    [46, 50],
                ],
                { fill: C.orange, sw: 2, seed: "dkk" },
            ) +
            Dt(55, 41, 1.8),
        tshirt: () =>
            Pg(
                [
                    [42, 20],
                    [58, 14],
                    [82, 14],
                    [98, 20],
                    [118, 40],
                    [104, 52],
                    [96, 44],
                    [96, 98],
                    [44, 98],
                    [44, 44],
                    [36, 52],
                    [22, 40],
                ],
                { fill: "#FFF", seed: "ts" },
            ) + T("병", 70, 46, 30, C.red, null, "middle", 13),
        snowman: () =>
            S(E(70, 74, 30, 26), { fill: "#FFF", seed: "sn1" }) +
            S(E(70, 34, 20, 18), { fill: "#FFF", seed: "sn2" }) +
            Dt(63, 30, 2) +
            Dt(77, 30, 2) +
            Pg(
                [
                    [70, 36],
                    [86, 39],
                    [70, 41],
                ],
                { fill: C.orange, sw: 2, seed: "snn" },
            ),
        socks: () =>
            Pg(
                [
                    [30, 14],
                    [56, 14],
                    [56, 70],
                    [74, 84],
                    [66, 98],
                    [30, 92],
                ],
                { fill: "#FFF", seed: "so1" },
            ) +
            Pg(
                [
                    [70, 14],
                    [96, 14],
                    [96, 70],
                    [114, 84],
                    [106, 98],
                    [70, 92],
                ],
                { fill: C.sky, seed: "so2" },
            ) +
            Ln(
                [
                    [30, 30],
                    [56, 30],
                ],
                { sw: 4, stroke: C.red },
            ) +
            Ln(
                [
                    [70, 30],
                    [96, 30],
                ],
                { sw: 4, stroke: C.red },
            ),
        egg: () =>
            S(E(70, 58, 30, 40), { fill: "#FFF6E0", seed: "eg" }) +
            Ln(
                [
                    [50, 54],
                    [60, 48],
                    [66, 58],
                    [76, 48],
                    [86, 56],
                ],
                { sw: 2.5 },
            ),
        potato: () =>
            Pg(
                [
                    [34, 50],
                    [44, 24],
                    [80, 18],
                    [106, 36],
                    [110, 70],
                    [90, 96],
                    [52, 94],
                    [32, 76],
                ],
                { fill: "#C49A5A", seed: "pt" },
            ) + F(72, 56, 26, "ptf", "open"),
        pants: () =>
            Pg(
                [
                    [36, 14],
                    [104, 14],
                    [112, 98],
                    [80, 98],
                    [70, 44],
                    [60, 98],
                    [28, 98],
                ],
                { fill: C.pink, seed: "pn" },
            ),
        phone: () =>
            S(R(46, 8, 48, 94, 10), { fill: "#555", seed: "ph" }) +
            S(R(54, 18, 32, 30, 3), { fill: C.sky, sw: 3, seed: "phs" }) +
            [0, 1, 2]
                .map((r) =>
                    [0, 1, 2]
                        .map((c) => Dt(58 + c * 12, 60 + r * 12, 3, "#DDD"))
                        .join(""),
                )
                .join(""),
        dog: () =>
            at(
                70,
                60,
                1.8,
                Ln(
                    [
                        [-14, 8],
                        [-14, 18],
                    ],
                    { sw: 3 },
                ) +
                    Ln(
                        [
                            [12, 8],
                            [12, 18],
                        ],
                        { sw: 3 },
                    ) +
                    S(E(0, 2, 20, 11), { fill: "#C8823A", seed: "dg" }) +
                    S(E(-20, -8, 11, 10), { fill: "#C8823A", seed: "dgh" }) +
                    S(E(-26, -14, 4, 7), {
                        fill: C.brown,
                        sw: 2,
                        seed: "dge",
                    }) +
                    Dt(-22, -10, 1.6) +
                    Ln(
                        [
                            [20, -2],
                            [27, -10],
                        ],
                        { sw: 3 },
                    ),
            ),
        limo: () => at(70, 62, 1, cr.car("#222", "lm", 60)),
        bird: () => at(76, 56, 2.4, cr.bird(C.olive, "pb")),
        bag: () =>
            Ln(A(70, 38, 20, 18, Math.PI, Math.PI * 2, 8), { sw: 4 }) +
            S(R(36, 36, 68, 58, 8), { fill: C.red, seed: "bg" }) +
            Ln(
                [
                    [110, 20],
                    [120, 12],
                ],
                { sw: 3, stroke: C.tan },
            ) +
            Ln(
                [
                    [116, 34],
                    [128, 32],
                ],
                { sw: 3, stroke: C.tan },
            ) +
            Ln(
                [
                    [24, 20],
                    [16, 12],
                ],
                { sw: 3, stroke: C.tan },
            ),
    };
    art.product = (key, w, h) =>
        `<svg class="d" viewBox="0 0 140 110" width="${w || 140}" height="${h || 110}" aria-hidden="true">${PROD[key]()}</svg>`;

    art.cr = cr;
    BM.art = art;
})(typeof window !== "undefined" ? window : globalThis);
