/** stageLabel converts a backend stage into product vocabulary (FR-10.11). */
export function stageLabel(stage: string | null): string {
  if (!stage) return "Building the review";
  if (stage.startsWith("applying:")) return `Applying #${stage.slice("applying:".length)}`;
  switch (stage) {
    case "resolving":
      return "Resolving PRs/MRs";
    case "updating-repository":
      return "Updating repository";
    case "creating-workspace":
      return "Preparing review";
    case "diffing":
      return "Computing the combined diff";
    default:
      return "Building the review";
  }
}
