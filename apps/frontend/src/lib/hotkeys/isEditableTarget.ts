const EDITABLE_TAGS = new Set(["INPUT", "TEXTAREA", "SELECT"]);

/**
 * isEditableTarget reports whether a key press belongs to the user's typing
 * rather than to a page shortcut (FR-41). `isContentEditable` is only defined
 * for elements attached to a document, so the attribute is checked too.
 */
export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (EDITABLE_TAGS.has(target.tagName)) return true;
  return target.isContentEditable || target.getAttribute("contenteditable") === "true";
}
