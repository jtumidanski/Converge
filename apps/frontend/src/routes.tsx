import { Route, Routes } from "react-router";
import { ReviewsPage } from "@/pages/ReviewsPage";
import { SelectChangesPage } from "@/pages/SelectChangesPage";
import { ReviewPage } from "@/pages/ReviewPage";
import { EmptyState } from "@/components/common/EmptyState";

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<ReviewsPage />} />
      <Route path="/select" element={<SelectChangesPage />} />
      <Route path="/reviews/:id" element={<ReviewPage />} />
      <Route
        path="*"
        element={<EmptyState title="Page not found" description="That address does not exist." />}
      />
    </Routes>
  );
}
