// Adds a Copy button to every command box (its label can be set with
// data-label). Without JavaScript the commands can still be selected and
// copied by hand.
for (const box of document.querySelectorAll('.cmd')) {
  const pre = box.querySelector('pre');
  const label = box.dataset.label || 'Copy';
  const button = document.createElement('button');
  const icon = (name) => `<svg class="icon" aria-hidden="true"><use href="/icons.svg#${name}"/></svg>`;
  const show = (name, text) => { button.innerHTML = `${icon(name)}<span>${text}</span>`; };
  button.type = 'button';
  button.className = 'copy';
  show('copy', label);
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(pre.textContent.trim());
      show('check', 'Copied');
    } catch {
      getSelection().selectAllChildren(pre); // copying was blocked: select it to copy by hand
      show('copy', 'Selected, press Ctrl C');
    }
    setTimeout(() => show('copy', label), 1600);
  });
  box.append(button);
}
