// Mouse-style lettering: each Hangul syllable is composed from jamo stroke
// templates and drawn as a wobbly SVG path. No font is used.
((root) => {
    "use strict";
    root.BM = root.BM || {};
    const BM = root.BM;

    // Seeded PRNG: the same string always gets the same wobble.
    function hash(s) {
        let h = 2166136261;
        for (let i = 0; i < s.length; i++) {
            h ^= s.charCodeAt(i);
            h = Math.imul(h, 16777619);
        }
        return h >>> 0;
    }
    function rng(seed) {
        let a = typeof seed === "number" ? seed >>> 0 : hash(String(seed));
        return () => {
            a = (a + 0x6d2b79f5) >>> 0;
            let t = a;
            t = Math.imul(t ^ (t >>> 15), t | 1);
            t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
            return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
        };
    }
    BM.hash = hash;
    BM.rng = rng;

    function ring(cx, cy, rx, ry) {
        const p = [];
        for (let i = 0; i <= 16; i++) {
            const a = -Math.PI / 2 + (i * Math.PI) / 8;
            p.push([cx + rx * Math.cos(a), cy + ry * Math.sin(a)]);
        }
        return [p];
    }

    // Consonants in a unit box. Each entry is a list of polylines.
    const CONS = {
        ㄱ: [
            [
                [0.08, 0.12],
                [0.9, 0.12],
                [0.82, 0.95],
            ],
        ],
        ㄴ: [
            [
                [0.12, 0.05],
                [0.12, 0.88],
                [0.95, 0.88],
            ],
        ],
        ㄷ: [
            [
                [0.9, 0.1],
                [0.12, 0.1],
                [0.12, 0.88],
                [0.95, 0.88],
            ],
        ],
        ㄹ: [
            [
                [0.1, 0.08],
                [0.88, 0.08],
                [0.88, 0.47],
                [0.12, 0.47],
                [0.12, 0.9],
                [0.95, 0.9],
            ],
        ],
        ㅁ: [
            [
                [0.12, 0.1],
                [0.12, 0.9],
                [0.88, 0.9],
                [0.88, 0.1],
                [0.12, 0.1],
            ],
        ],
        ㅂ: [
            [
                [0.12, 0.04],
                [0.12, 0.92],
                [0.88, 0.92],
                [0.88, 0.04],
            ],
            [
                [0.12, 0.46],
                [0.88, 0.46],
            ],
        ],
        ㅅ: [
            [
                [0.52, 0.05],
                [0.08, 0.95],
            ],
            [
                [0.42, 0.42],
                [0.94, 0.95],
            ],
        ],
        ㅇ: ring(0.5, 0.5, 0.4, 0.42),
        ㅈ: [
            [
                [0.1, 0.1],
                [0.88, 0.1],
            ],
            [
                [0.58, 0.12],
                [0.08, 0.95],
            ],
            [
                [0.42, 0.48],
                [0.94, 0.95],
            ],
        ],
        ㅊ: [
            [
                [0.42, 0.0],
                [0.6, 0.1],
            ],
            [
                [0.1, 0.24],
                [0.88, 0.24],
            ],
            [
                [0.56, 0.26],
                [0.08, 0.95],
            ],
            [
                [0.42, 0.58],
                [0.94, 0.95],
            ],
        ],
        ㅋ: [
            [
                [0.08, 0.12],
                [0.9, 0.12],
                [0.82, 0.95],
            ],
            [
                [0.12, 0.52],
                [0.86, 0.52],
            ],
        ],
        ㅌ: [
            [
                [0.9, 0.08],
                [0.12, 0.08],
                [0.12, 0.9],
                [0.95, 0.9],
            ],
            [
                [0.12, 0.48],
                [0.84, 0.48],
            ],
        ],
        ㅍ: [
            [
                [0.04, 0.1],
                [0.96, 0.1],
            ],
            [
                [0.32, 0.12],
                [0.28, 0.88],
            ],
            [
                [0.68, 0.12],
                [0.72, 0.88],
            ],
            [
                [0.04, 0.9],
                [0.96, 0.9],
            ],
        ],
        ㅎ: [
            [
                [0.4, 0.0],
                [0.6, 0.07],
            ],
            [
                [0.1, 0.2],
                [0.9, 0.2],
            ],
        ].concat(ring(0.5, 0.64, 0.3, 0.28)),
    };
    const PAIRS = {
        ㄲ: "ㄱㄱ",
        ㄸ: "ㄷㄷ",
        ㅃ: "ㅂㅂ",
        ㅆ: "ㅅㅅ",
        ㅉ: "ㅈㅈ",
        ㄳ: "ㄱㅅ",
        ㄵ: "ㄴㅈ",
        ㄶ: "ㄴㅎ",
        ㄺ: "ㄹㄱ",
        ㄻ: "ㄹㅁ",
        ㄼ: "ㄹㅂ",
        ㄽ: "ㄹㅅ",
        ㄾ: "ㄹㅌ",
        ㄿ: "ㄹㅍ",
        ㅀ: "ㄹㅎ",
        ㅄ: "ㅂㅅ",
    };
    const LEAD = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ";
    const VOW = "ㅏㅐㅑㅒㅓㅔㅕㅖㅗㅘㅙㅚㅛㅜㅝㅞㅟㅠㅡㅢㅣ";
    const TAIL = [
        "",
        ..."ㄱㄲㄳㄴㄵㄶㄷㄹㄺㄻㄼㄽㄾㄿㅀㅁㅂㅄㅅㅆㅇㅈㅊㅋㅌㅍㅎ",
    ];
    const MIX = {
        ㅘ: "ㅗㅏ",
        ㅙ: "ㅗㅐ",
        ㅚ: "ㅗㅣ",
        ㅝ: "ㅜㅓ",
        ㅞ: "ㅜㅔ",
        ㅟ: "ㅜㅣ",
        ㅢ: "ㅡㅣ",
    };
    const HORIZ = "ㅗㅛㅜㅠㅡ";

    // Boxes in a 100 x 100 em, per vowel class (v/h/m) and final (0/1).
    const LAYOUT = {
        v0: { L: [6, 14, 54, 86], top: 4, bot: 96 },
        v1: { L: [6, 6, 54, 48], top: 2, bot: 60, T: [14, 64, 86, 97] },
        h0: { L: [16, 4, 84, 48], band: [54, 92], hx: [6, 94] },
        h1: {
            L: [20, 2, 80, 32],
            band: [36, 60],
            hx: [8, 92],
            T: [16, 64, 84, 97],
        },
        m0: { L: [6, 4, 56, 44], band: [50, 88], hx: [4, 62], top: 2, bot: 98 },
        m1: {
            L: [6, 2, 54, 28],
            band: [32, 58],
            hx: [4, 62],
            top: 0,
            bot: 62,
            T: [12, 66, 78, 97],
        },
    };

    function cons(ch, box) {
        const [x0, y0, x1, y1] = box;
        if (PAIRS[ch]) {
            const m = (x0 + x1) / 2;
            return cons(PAIRS[ch][0], [x0, y0, m - 2, y1]).concat(
                cons(PAIRS[ch][1], [m + 2, y0, x1, y1]),
            );
        }
        return CONS[ch].map((s) =>
            s.map(([u, v]) => [x0 + u * (x1 - x0), y0 + v * (y1 - y0)]),
        );
    }

    function vert(v, X, t, b) {
        const s = [];
        const m = (t + b) / 2;
        const d = (b - t) * 0.13;
        const line = (x) =>
            s.push([
                [x, t],
                [x, b],
            ]);
        const ticks = (from, to, two) => {
            if (two)
                s.push(
                    [
                        [from, m - d],
                        [to, m - d],
                    ],
                    [
                        [from, m + d],
                        [to, m + d],
                    ],
                );
            else
                s.push([
                    [from, m],
                    [to, m],
                ]);
        };
        if (v === "ㅣ") line(X);
        else if (v === "ㅏ" || v === "ㅑ") {
            line(X);
            ticks(X, X + 14, v === "ㅑ");
        } else if (v === "ㅓ" || v === "ㅕ") {
            line(X);
            ticks(X - 14, X, v === "ㅕ");
        } else if (v === "ㅐ" || v === "ㅒ") {
            line(X - 8);
            ticks(X - 8, X + 8, v === "ㅒ");
            line(X + 8);
        } else if (v === "ㅔ" || v === "ㅖ") {
            ticks(X - 20, X - 6, v === "ㅖ");
            line(X - 6);
            line(X + 10);
        }
        return s;
    }

    function horiz(v, x0, x1, t, b, cx) {
        if (v === "ㅡ")
            return [
                [
                    [x0, (t + b) / 2],
                    [x1, (t + b) / 2],
                ],
            ];
        const up = v === "ㅗ" || v === "ㅛ";
        const yl = up ? b - (b - t) * 0.2 : t + (b - t) * 0.2;
        const ys = up ? t : b;
        const xs = v === "ㅛ" || v === "ㅠ" ? [cx - 10, cx + 10] : [cx];
        return [
            [
                [x0, yl],
                [x1, yl],
            ],
        ].concat(
            xs.map((x) => [
                [x, ys],
                [x, yl],
            ]),
        );
    }

    function decompose(code) {
        return [
            LEAD[(code / 588) | 0],
            VOW[((code % 588) / 28) | 0],
            TAIL[code % 28],
        ];
    }

    function syllable(code) {
        const [L, V, T] = decompose(code);
        const cls = HORIZ.includes(V) ? "h" : MIX[V] ? "m" : "v";
        const lay = LAYOUT[cls + (T ? 1 : 0)];
        let s;
        if (cls === "v") {
            const box = lay.L.slice();
            if ("ㅓㅕㅔㅖ".includes(V)) box[2] -= 6;
            s = cons(L, box).concat(vert(V, 72, lay.top, lay.bot));
        } else if (cls === "h") {
            s = cons(L, lay.L).concat(
                horiz(V, lay.hx[0], lay.hx[1], lay.band[0], lay.band[1], 50),
            );
        } else {
            const [h, vp] = MIX[V];
            s = cons(L, lay.L).concat(
                horiz(h, lay.hx[0], lay.hx[1], lay.band[0], lay.band[1], 34),
                vert(vp, 80, lay.top, lay.bot),
            );
        }
        return T ? s.concat(cons(T, lay.T)) : s;
    }

    // A standalone jamo such as "ㅋㅋ" or "ㅠㅠ".
    function jamo(ch) {
        if (CONS[ch] || PAIRS[ch]) return cons(ch, [14, 10, 86, 90]);
        if (HORIZ.includes(ch)) return horiz(ch, 8, 92, 30, 70, 50);
        if (MIX[ch])
            return horiz(MIX[ch][0], 4, 66, 40, 70, 34).concat(
                vert(MIX[ch][1], 78, 6, 94),
            );
        return vert(ch, 46, 6, 94);
    }

    // Latin, digits and marks: [advance, polylines], y from 10 (top) to 90 (base).
    const o = (cx, cy, rx, ry) => ring(cx, cy, rx, ry)[0];
    const GLYPH = {
        A: [
            60,
            [
                [
                    [0, 90],
                    [30, 10],
                    [60, 90],
                ],
                [
                    [12, 60],
                    [48, 60],
                ],
            ],
        ],
        B: [
            56,
            [
                [
                    [5, 90],
                    [5, 10],
                    [40, 10],
                    [52, 20],
                    [52, 38],
                    [40, 48],
                    [5, 48],
                ],
                [
                    [40, 48],
                    [56, 58],
                    [56, 80],
                    [44, 90],
                    [5, 90],
                ],
            ],
        ],
        C: [
            56,
            [
                [
                    [55, 22],
                    [40, 10],
                    [20, 10],
                    [5, 30],
                    [5, 70],
                    [20, 90],
                    [40, 90],
                    [55, 78],
                ],
            ],
        ],
        D: [
            58,
            [
                [
                    [5, 10],
                    [5, 90],
                    [35, 90],
                    [55, 70],
                    [55, 30],
                    [35, 10],
                    [5, 10],
                ],
            ],
        ],
        E: [
            54,
            [
                [
                    [54, 10],
                    [5, 10],
                    [5, 90],
                    [54, 90],
                ],
                [
                    [5, 50],
                    [44, 50],
                ],
            ],
        ],
        F: [
            52,
            [
                [
                    [54, 10],
                    [5, 10],
                    [5, 90],
                ],
                [
                    [5, 50],
                    [44, 50],
                ],
            ],
        ],
        G: [
            58,
            [
                [
                    [55, 22],
                    [40, 10],
                    [20, 10],
                    [5, 30],
                    [5, 70],
                    [20, 90],
                    [45, 90],
                    [55, 75],
                    [55, 55],
                    [35, 55],
                ],
            ],
        ],
        H: [
            58,
            [
                [
                    [5, 10],
                    [5, 90],
                ],
                [
                    [55, 10],
                    [55, 90],
                ],
                [
                    [5, 50],
                    [55, 50],
                ],
            ],
        ],
        I: [
            40,
            [
                [
                    [20, 10],
                    [20, 90],
                ],
                [
                    [5, 10],
                    [35, 10],
                ],
                [
                    [5, 90],
                    [35, 90],
                ],
            ],
        ],
        J: [
            54,
            [
                [
                    [50, 10],
                    [50, 75],
                    [38, 90],
                    [20, 90],
                    [6, 75],
                ],
            ],
        ],
        K: [
            56,
            [
                [
                    [5, 10],
                    [5, 90],
                ],
                [
                    [55, 10],
                    [5, 58],
                ],
                [
                    [22, 42],
                    [55, 90],
                ],
            ],
        ],
        L: [
            50,
            [
                [
                    [5, 10],
                    [5, 90],
                    [50, 90],
                ],
            ],
        ],
        M: [
            64,
            [
                [
                    [3, 90],
                    [8, 10],
                    [32, 62],
                    [56, 10],
                    [61, 90],
                ],
            ],
        ],
        N: [
            58,
            [
                [
                    [5, 90],
                    [5, 10],
                    [55, 90],
                    [55, 10],
                ],
            ],
        ],
        O: [60, [o(30, 50, 27, 40)]],
        P: [
            54,
            [
                [
                    [5, 90],
                    [5, 10],
                    [42, 10],
                    [55, 22],
                    [55, 40],
                    [42, 52],
                    [5, 52],
                ],
            ],
        ],
        Q: [
            60,
            [
                o(30, 50, 27, 40),
                [
                    [35, 70],
                    [58, 95],
                ],
            ],
        ],
        R: [
            56,
            [
                [
                    [5, 90],
                    [5, 10],
                    [42, 10],
                    [55, 22],
                    [55, 40],
                    [42, 52],
                    [5, 52],
                ],
                [
                    [30, 52],
                    [56, 90],
                ],
            ],
        ],
        S: [
            56,
            [
                [
                    [55, 20],
                    [42, 10],
                    [18, 10],
                    [5, 22],
                    [8, 40],
                    [30, 50],
                    [52, 60],
                    [55, 78],
                    [42, 90],
                    [15, 90],
                    [3, 78],
                ],
            ],
        ],
        T: [
            58,
            [
                [
                    [0, 10],
                    [58, 10],
                ],
                [
                    [29, 10],
                    [29, 90],
                ],
            ],
        ],
        U: [
            58,
            [
                [
                    [5, 10],
                    [5, 72],
                    [18, 90],
                    [40, 90],
                    [53, 72],
                    [53, 10],
                ],
            ],
        ],
        V: [
            60,
            [
                [
                    [0, 10],
                    [30, 90],
                    [60, 10],
                ],
            ],
        ],
        W: [
            70,
            [
                [
                    [0, 10],
                    [17, 90],
                    [35, 40],
                    [53, 90],
                    [70, 10],
                ],
            ],
        ],
        X: [
            58,
            [
                [
                    [5, 10],
                    [55, 90],
                ],
                [
                    [55, 10],
                    [5, 90],
                ],
            ],
        ],
        Y: [
            60,
            [
                [
                    [3, 10],
                    [30, 50],
                    [57, 10],
                ],
                [
                    [30, 50],
                    [30, 90],
                ],
            ],
        ],
        Z: [
            58,
            [
                [
                    [5, 10],
                    [55, 10],
                    [5, 90],
                    [55, 90],
                ],
            ],
        ],
        0: [56, [o(28, 50, 24, 40)]],
        1: [
            44,
            [
                [
                    [10, 25],
                    [26, 10],
                    [26, 90],
                ],
                [
                    [10, 90],
                    [42, 90],
                ],
            ],
        ],
        2: [
            56,
            [
                [
                    [5, 25],
                    [18, 10],
                    [42, 10],
                    [55, 25],
                    [52, 45],
                    [5, 90],
                    [57, 90],
                ],
            ],
        ],
        3: [
            54,
            [
                [
                    [5, 15],
                    [50, 10],
                    [28, 45],
                    [52, 58],
                    [54, 78],
                    [40, 90],
                    [8, 88],
                ],
            ],
        ],
        4: [
            58,
            [
                [
                    [42, 90],
                    [42, 10],
                    [3, 65],
                    [58, 65],
                ],
            ],
        ],
        5: [
            56,
            [
                [
                    [52, 10],
                    [10, 10],
                    [6, 45],
                    [35, 40],
                    [55, 55],
                    [55, 78],
                    [40, 90],
                    [6, 86],
                ],
            ],
        ],
        6: [
            56,
            [
                [
                    [50, 12],
                    [25, 15],
                    [8, 45],
                    [8, 75],
                    [25, 90],
                    [45, 88],
                    [55, 70],
                    [45, 52],
                    [25, 50],
                    [8, 62],
                ],
            ],
        ],
        7: [
            54,
            [
                [
                    [5, 10],
                    [55, 10],
                    [22, 90],
                ],
            ],
        ],
        8: [56, [o(28, 30, 20, 20), o(28, 70, 25, 20)]],
        9: [
            56,
            [
                [
                    [52, 40],
                    [38, 52],
                    [18, 50],
                    [6, 32],
                    [15, 12],
                    [38, 10],
                    [52, 25],
                    [50, 60],
                    [35, 90],
                    [12, 88],
                ],
            ],
        ],
        ".": [
            22,
            [
                [
                    [10, 86],
                    [12, 88],
                ],
            ],
        ],
        ",": [
            22,
            [
                [
                    [14, 84],
                    [8, 98],
                ],
            ],
        ],
        "!": [
            24,
            [
                [
                    [12, 10],
                    [12, 64],
                ],
                [
                    [12, 86],
                    [12, 88],
                ],
            ],
        ],
        "?": [
            52,
            [
                [
                    [6, 25],
                    [18, 10],
                    [38, 10],
                    [48, 25],
                    [28, 50],
                    [28, 64],
                ],
                [
                    [28, 86],
                    [28, 88],
                ],
            ],
        ],
        "~": [
            60,
            [
                [
                    [3, 55],
                    [15, 42],
                    [30, 52],
                    [45, 60],
                    [57, 45],
                ],
            ],
        ],
        "-": [
            44,
            [
                [
                    [6, 52],
                    [38, 52],
                ],
            ],
        ],
        "(": [
            32,
            [
                [
                    [28, 5],
                    [8, 30],
                    [8, 70],
                    [28, 95],
                ],
            ],
        ],
        ")": [
            32,
            [
                [
                    [4, 5],
                    [24, 30],
                    [24, 70],
                    [4, 95],
                ],
            ],
        ],
        "[": [
            30,
            [
                [
                    [26, 5],
                    [8, 5],
                    [8, 95],
                    [26, 95],
                ],
            ],
        ],
        "]": [
            30,
            [
                [
                    [4, 5],
                    [22, 5],
                    [22, 95],
                    [4, 95],
                ],
            ],
        ],
        ":": [
            22,
            [
                [
                    [11, 40],
                    [11, 42],
                ],
                [
                    [11, 84],
                    [11, 86],
                ],
            ],
        ],
        "/": [
            44,
            [
                [
                    [40, 5],
                    [4, 95],
                ],
            ],
        ],
        "^": [
            44,
            [
                [
                    [4, 40],
                    [22, 15],
                    [40, 40],
                ],
            ],
        ],
        ">": [
            44,
            [
                [
                    [6, 25],
                    [40, 50],
                    [6, 75],
                ],
            ],
        ],
        "<": [
            44,
            [
                [
                    [40, 25],
                    [6, 50],
                    [40, 75],
                ],
            ],
        ],
        "+": [
            48,
            [
                [
                    [24, 30],
                    [24, 74],
                ],
                [
                    [4, 52],
                    [44, 52],
                ],
            ],
        ],
        "*": [
            44,
            [
                [
                    [22, 20],
                    [22, 60],
                ],
                [
                    [6, 28],
                    [38, 52],
                ],
                [
                    [38, 28],
                    [6, 52],
                ],
            ],
        ],
        "'": [
            18,
            [
                [
                    [9, 8],
                    [9, 30],
                ],
            ],
        ],
        '"': [
            30,
            [
                [
                    [8, 8],
                    [8, 30],
                ],
                [
                    [22, 8],
                    [22, 30],
                ],
            ],
        ],
        "·": [
            26,
            [
                [
                    [12, 50],
                    [14, 52],
                ],
            ],
        ],
        "…": [
            60,
            [
                [
                    [10, 86],
                    [12, 88],
                ],
                [
                    [30, 86],
                    [32, 88],
                ],
                [
                    [50, 86],
                    [52, 88],
                ],
            ],
        ],
        "♡": [
            62,
            [
                [
                    [31, 35],
                    [20, 20],
                    [6, 26],
                    [6, 46],
                    [31, 82],
                    [56, 46],
                    [56, 26],
                    [42, 20],
                    [31, 35],
                ],
            ],
        ],
        "♥": [
            62,
            [
                [
                    [31, 35],
                    [20, 20],
                    [6, 26],
                    [6, 46],
                    [31, 82],
                    [56, 46],
                    [56, 26],
                    [42, 20],
                    [31, 35],
                ],
            ],
            true,
        ],
    };

    // Layout in em units (100 = one Hangul box). Shared by both renderers.
    function layout(str, seed) {
        const r = rng(seed);
        const out = [];
        let x = 0;
        for (const ch of str) {
            const code = ch.charCodeAt(0);
            const g = {
                ch,
                x,
                rot: (r() - 0.5) * 10,
                dy: (r() - 0.5) * 9,
                sc: 0.92 + r() * 0.14,
            };
            if (code >= 0xac00 && code <= 0xd7a3) {
                g.kind = "syl";
                g.w = 96;
            } else if (code >= 0x3131 && code <= 0x318e) {
                g.kind = "jamo";
                g.w = 90;
            } else if (ch === " ") {
                g.kind = "space";
                g.w = 38;
            } else if (GLYPH[ch.toUpperCase()]) {
                // Lowercase is drawn as a small capital on the same baseline.
                const small = ch !== ch.toUpperCase();
                g.kind = "latin";
                g.w = GLYPH[ch.toUpperCase()][0] * (small ? 0.75 : 1) + 12;
                if (small) {
                    g.sc *= 0.75;
                    g.dy += 10;
                }
            } else {
                g.kind = "other";
                g.w = 80;
            }
            x += g.w;
            out.push(g);
        }
        return { glyphs: out, width: Math.max(0, x - 8) };
    }

    function strokesOf(g) {
        if (g.kind === "syl") return syllable(g.ch.charCodeAt(0) - 0xac00);
        if (g.kind === "jamo") return jamo(g.ch);
        if (g.kind === "latin") return GLYPH[g.ch.toUpperCase()][1];
        return [];
    }

    // A hand on a mouse: the line drifts slowly, it does not shake.
    function rough(pts, r, amp) {
        const out = [];
        let ox = 0;
        let oy = 0;
        const push = (x, y) => {
            ox = ox * 0.55 + (r() - 0.5) * amp;
            oy = oy * 0.55 + (r() - 0.5) * amp;
            out.push([x + ox, y + oy]);
        };
        for (let i = 0; i < pts.length - 1; i++) {
            const [ax, ay] = pts[i];
            const [bx, by] = pts[i + 1];
            const n = Math.max(1, Math.round(Math.hypot(bx - ax, by - ay) / 9));
            for (let j = 0; j < n; j++)
                push(ax + ((bx - ax) * j) / n, ay + ((by - ay) * j) / n);
        }
        const last = pts[pts.length - 1];
        push(last[0], last[1]);
        return out;
    }

    const f1 = (n) => Math.round(n * 10) / 10;
    const dOf = (pts) =>
        `M${pts.map((p) => `${f1(p[0])} ${f1(p[1])}`).join("L")}`;

    const esc = (s) =>
        String(s).replace(
            /[&<>"]/g,
            (c) =>
                ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c],
        );

    // Lettering as an SVG <g>. o: size (px per em), color, outline (color or
    // null), weight (stroke in em units), seed, x, y (top-left), anchor.
    function letter(str, opt) {
        const op = Object.assign(
            {
                size: 24,
                color: "#000",
                outline: null,
                weight: 11,
                x: 0,
                y: 0,
                anchor: "start",
            },
            opt,
        );
        const seed = op.seed == null ? str : op.seed;
        const lay = layout(str, seed);
        const k = op.size / 100;
        const w = lay.width * k;
        const x0 =
            op.x -
            (op.anchor === "middle" ? w / 2 : op.anchor === "end" ? w : 0);
        const ow = op.outline ? op.weight + 12 : 0;
        const label = ` aria-hidden="true"`;
        const r = rng(`${seed}#`);
        const lines = [];
        const fills = [];
        const extra = [];
        lay.glyphs.forEach((g) => {
            const cx = g.x + g.w / 2;
            const a = (g.rot * Math.PI) / 180;
            const ca = Math.cos(a) * g.sc;
            const sa = Math.sin(a) * g.sc;
            const map = ([px, py]) => {
                const dx = g.x + px - cx;
                const dy = py - 50;
                return [
                    x0 + (cx + dx * ca - dy * sa) * k,
                    op.y + (50 + g.dy + dx * sa + dy * ca) * k,
                ];
            };
            if (g.kind === "other") {
                extra.push(
                    `<text x="${f1(x0 + g.x * k)}" y="${f1(op.y + 86 * k)}" font-size="${f1(op.size)}" fill="${op.color}">${esc(g.ch)}</text>`,
                );
                return;
            }
            for (const s of strokesOf(g)) {
                lines.push(dOf(rough(s.map(map), r, 8 * k)));
            }
            if (g.kind === "latin" && GLYPH[g.ch.toUpperCase()][2])
                fills.push(`${dOf(GLYPH[g.ch.toUpperCase()][1][0].map(map))}Z`);
        });
        const d = lines.join("");
        const sw = op.weight * k;
        let out = `<g class="ltr"${label} fill="none" stroke-linecap="round" stroke-linejoin="round">`;
        if (fills.length)
            out += `<path d="${fills.join("")}" fill="${op.color}" stroke="none"/>`;
        if (op.outline)
            out += `<path d="${d}" stroke="${op.outline}" stroke-width="${f1(ow * k)}"/>`;
        out += `<path d="${d}" stroke="${op.color}" stroke-width="${f1(sw)}"/>`;
        return `${out + extra.join("")}</g>`;
    }

    BM.letter = letter;
    BM.measure = (str, size) => (layout(str, str).width * size) / 100;
    BM.esc = esc;
    BM._test = { decompose, syllable, jamo, layout, LEAD, VOW, TAIL };
})(typeof window !== "undefined" ? window : globalThis);
