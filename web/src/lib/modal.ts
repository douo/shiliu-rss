/** Restore focus and keep keyboard navigation inside an active modal. */
export function modal(node: HTMLElement, close: () => void) {
  const previous = document.activeElement as HTMLElement | null;
  const focusable = () =>
    Array.from(
      node.querySelectorAll<HTMLElement>(
        'button:not(:disabled),a[href],input:not(:disabled),textarea,select,[tabindex="0"]'
      )
    );
  queueMicrotask(() => {
    (focusable()[0] || node).focus();
  });
  const keydown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
    }
    if (e.key === 'Tab') {
      const elements = focusable();
      const first = elements[0],
        last = elements.at(-1);
      if (!first) {
        e.preventDefault();
        node.focus();
      } else if (
        e.shiftKey &&
        (document.activeElement === first || document.activeElement === node)
      ) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  };
  node.addEventListener('keydown', keydown);
  return {
    destroy() {
      node.removeEventListener('keydown', keydown);
      previous?.focus();
    }
  };
}
