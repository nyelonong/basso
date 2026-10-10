import "@fontsource-variable/archivo";
import "@fontsource/fragment-mono";
import "./styles.css";

const installCommand = "go install github.com/nyelonong/basso/cmd/basso@latest";

function required<T extends Element>(selector: string, root: ParentNode = document): T {
  const element = root.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing required element: ${selector}`);
  }
  return element;
}

const copyButton = required<HTMLButtonElement>("[data-copy-install]");
const copyStatus = required<HTMLElement>("[data-copy-status]");

async function copyText(value: string) {
  if (navigator.clipboard) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const field = document.createElement("textarea");
  field.value = value;
  field.style.position = "fixed";
  field.style.opacity = "0";
  document.body.append(field);
  field.select();
  document.execCommand("copy");
  field.remove();
}

copyButton.addEventListener("click", async () => {
  try {
    await copyText(installCommand);
    copyButton.textContent = "Copied";
    copyStatus.textContent = "Install command copied. Then save the file on any step.";
    window.setTimeout(() => {
      copyButton.textContent = "Copy";
      copyStatus.textContent = "Then save the file on any step.";
    }, 2_000);
  } catch (error) {
    copyStatus.textContent = "Copy failed. Select the command manually.";
    console.error(error);
  }
});
