import { adminPasswordHash } from "./crypto.js";
import { settlePasswordInput } from "./ime.js";

const form = document.getElementById("adminLoginForm");
const password = document.getElementById("adminPassword");
const passwordHash = document.getElementById("adminPasswordHash");
let composing = false;

password.addEventListener("compositionstart", () => {
	composing = true;
});
password.addEventListener("compositionend", () => {
	composing = false;
});

form.addEventListener("submit", async (event) => {
	event.preventDefault();
	await settlePasswordInput(password, () => composing);
	passwordHash.value = await adminPasswordHash(password.value);
	password.value = "";
	form.submit();
});