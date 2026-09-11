import { createContext } from "react";

export interface BreadcrumbSegment {
  label: string;
  /** When set, the segment renders as a link. The last segment never links. */
  to?: string;
}

export type BreadcrumbSetter = (segments: BreadcrumbSegment[]) => void;

/**
 * Breadcrumbs are published by pages rather than inferred from the URL: the
 * review breadcrumb needs loaded data (the repository and the included change
 * numbers), which the shell would otherwise have to fetch a second time.
 *
 * Reads and writes are deliberately two separate contexts, not one combined
 * value. A publishing page (useBreadcrumbs) only ever needs the setter, never
 * the current segments; if both lived in one context object, that object's
 * identity would change every time the *displayed* segments changed, which
 * would re-render every publishing page too. A page that (like a real page
 * building its breadcrumb from loaded data) doesn't memoise its segments
 * array would then re-run its publish effect with a new array identity,
 * publish again, change the display segments again, and loop forever.
 * BreadcrumbSetterContext's value is a stable function reference for exactly
 * this reason: publishing pages must never re-render when the display
 * segments change.
 */
export const BreadcrumbSegmentsContext = createContext<BreadcrumbSegment[]>([]);
export const BreadcrumbSetterContext = createContext<BreadcrumbSetter | null>(null);
