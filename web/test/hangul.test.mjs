import { expect, test } from "bun:test";
import "../static/js/draw/hangul.js";

const BM = globalThis.BM;
const T = BM._test;
const parts = (ch) => T.decompose(ch.charCodeAt(0) - 0xac00);

test("jamo tables and known decompositions", () => {
    expect(T.LEAD.length).toBe(19);
    expect(T.VOW.length).toBe(21);
    expect(T.TAIL.length).toBe(28);
    expect(parts("병")).toEqual(["ㅂ", "ㅕ", "ㅇ"]);
    expect(parts("읽")).toEqual(["ㅇ", "ㅣ", "ㄺ"]);
    expect(parts("왜")).toEqual(["ㅇ", "ㅙ", ""]);
});

test("every syllable gives strokes inside the em box", () => {
    for (let code = 0; code < 11172; code++) {
        const strokes = T.syllable(code);
        expect(strokes.length).toBeGreaterThan(1);
        for (const line of strokes) {
            for (const [x, y] of line) {
                if (x < -1 || x > 101 || y < -1 || y > 101) {
                    throw Error(
                        `out of box: ${String.fromCharCode(0xac00 + code)}`,
                    );
                }
            }
        }
    }
});

test("lettering has no NaN and a seed-independent width", () => {
    const label = "Share:// 마을창고 404 ㅋㅋ ~♥!?";
    const path = BM.letter(label, {
        size: 30,
        outline: "#000",
        color: "#FFE400",
    });
    expect(path).not.toContain("NaN");
    expect(path).toContain("<path");
    expect(BM.measure("이장실", 40)).toBeGreaterThan(BM.measure("이장", 40));
    expect(T.layout(label, 1).width).toBe(T.layout(label, 2).width);
});
