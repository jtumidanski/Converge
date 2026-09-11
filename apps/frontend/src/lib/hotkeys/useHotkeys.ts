import { useEffect, useRef } from "react";
import { isEditableTarget } from "@/lib/hotkeys/isEditableTarget";

export interface HotkeyOptions {
  /** Pass false while a sheet, dialog, or popover owns the keyboard. */
  enabled?: boolean;
}

/**
 * useHotkeys binds single-key shortcuts on window.
 *
 * A binding is skipped when the hook is disabled, when any of ctrl/meta/alt is
 * held (those belong to the browser), when the event was already handled, or
 * when focus is inside something the user is typing into. Radix dialogs and
 * popovers already trap focus; callers additionally pass `enabled: false`
 * while one is open, which satisfies FR-41 from both directions.
 *
 * Bindings live in a ref so a caller can pass inline closures without the
 * listener resubscribing on every render.
 */
export function useHotkeys(
  bindings: Record<string, () => void>,
  options: HotkeyOptions = {},
): void {
  const enabled = options.enabled ?? true;
  const bindingsRef = useRef(bindings);

  useEffect(() => {
    bindingsRef.current = bindings;
  }, [bindings]);

  useEffect(() => {
    if (!enabled) return;
    function onKeyDown(event: KeyboardEvent): void {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.defaultPrevented) return;
      if (isEditableTarget(event.target)) return;
      const handler = bindingsRef.current[event.key];
      if (!handler) return;
      event.preventDefault();
      handler();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled]);
}
