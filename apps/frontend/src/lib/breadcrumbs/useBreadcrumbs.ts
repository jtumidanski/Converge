import { useContext, useEffect } from "react";
import { BreadcrumbSetterContext, type BreadcrumbSegment } from "@/lib/breadcrumbs/context";

/**
 * useBreadcrumbs publishes this page's breadcrumb trail and clears it on
 * unmount so a route that publishes nothing falls back to the default.
 *
 * Pass a memoised array: the effect depends on identity, and a fresh array
 * every render would set state on every render. It subscribes only to
 * BreadcrumbSetterContext (a stable setter), never to the displayed
 * segments, so a caller that publishes an unmemoised array republishes once
 * per its own render instead of looping forever.
 */
export function useBreadcrumbs(segments: BreadcrumbSegment[]): void {
  const setSegments = useContext(BreadcrumbSetterContext);
  useEffect(() => {
    if (!setSegments) return;
    setSegments(segments);
    return () => setSegments([]);
  }, [setSegments, segments]);
}
