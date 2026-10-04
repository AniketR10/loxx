// Adds a Copy button to every command box. Without JavaScript the commands
// can still be selected and copied by hand.
for (const box of document.querySelectorAll('.cmd')) {
  const pre = box.querySelector('pre');
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'copy';
  button.textContent = 'Copy';
  button.setAttribute('aria-label', 'Copy the command');
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(pre.textContent.trim());
      button.textContent = 'Copied';
      button.setAttribute('aria-label', 'Copied');
    } catch {
      getSelection().selectAllChildren(pre);
      button.textContent = 'Selected';
    }
    setTimeout(() => {
      button.textContent = 'Copy';
      button.setAttribute('aria-label', 'Copy the command');
    }, 1500);
  });
  box.append(button);
}
