import { describe, expect, test } from "bun:test";
import { Progress } from "../static/js/progress.js";

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function progressHarness() {
	let text = "";
	const el = {
		set textContent(v) {
			text = v;
		},
		get textContent() {
			return text;
		},
	};
	globalThis.performance = { now: () => Date.now() };
	return { el, progress: new Progress(el), text: () => text };
}

describe("Progress", () => {
	test("reset clears failed phase before retry success", async () => {
		const { progress, text } = progressHarness();
		progress.pulse("decrypt", 100, "working");
		progress.fail("decrypt", "wrong password");
		expect(text()).toContain("failed: wrong password");

		progress.reset();
		expect(text()).toBe("");
		progress.pulse("decrypt", 100, "working");
		progress.done("decrypt", 100);
		await wait(700);
		expect(text()).toMatch(/decrypt.*100%.*done/);
		expect(text()).not.toContain("failed");
	});

	test("immediate done is not overwritten by pending pulse", async () => {
		const { progress, text } = progressHarness();
		progress.pulse("unzip", 1000, "working");
		progress.done("unzip", 1000);
		await wait(700);
		expect(text()).toMatch(/unzip.*100%.*done/);
		expect(text()).not.toContain("working");
	});

	test("reset clears active pulse timers", async () => {
		const { progress, text } = progressHarness();
		progress.pulse("download", 500, "working");
		progress.reset();
		await wait(1200);
		expect(progress.lines.size).toBe(0);
		expect(text()).toBe("");
	});
});
