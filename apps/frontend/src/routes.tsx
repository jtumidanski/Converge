import { Route, Routes } from "react-router";
import { SelectRepositoryPage } from "@/pages/SelectRepositoryPage";
import { SelectChangesPage } from "@/pages/SelectChangesPage";
import { ReviewPage } from "@/pages/ReviewPage";
import { EmptyState } from "@/components/common/EmptyState";

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<SelectRepositoryPage />} />
      <Route path="/select" element={<SelectChangesPage />} />
      <Route path="/reviews/:id" element={<ReviewPage />} />
      <Route
        path="*"
        element={
          <div className="mx-auto max-w-3xl p-10">
            <EmptyState title="Page not found" description="That address does not exist." />
          </div>
        }
      />
    </Routes>
  );
}
