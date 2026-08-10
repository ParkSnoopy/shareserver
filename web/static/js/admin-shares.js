const selectAll = document.getElementById("selectAllShares");
const deleteSelected = document.getElementById("deleteSelectedShares");
const selectors = Array.from(document.querySelectorAll(".share-selector"));

function updateSelection() {
	const selected = selectors.filter((selector) => selector.checked).length;
	deleteSelected.disabled = selected === 0;
	selectAll.checked = selectors.length > 0 && selected === selectors.length;
	selectAll.indeterminate = selected > 0 && selected < selectors.length;
}

selectAll.addEventListener("change", () => {
	for (const selector of selectors) selector.checked = selectAll.checked;
	updateSelection();
});

for (const selector of selectors) selector.addEventListener("change", updateSelection);
updateSelection();
