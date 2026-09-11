import { Fragment, useContext } from "react";
import { Link } from "react-router";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { BreadcrumbSegmentsContext } from "@/lib/breadcrumbs/context";
import { strings } from "@/lib/strings";

/** Breadcrumbs renders whatever the current page published, defaulting to Reviews. */
export function Breadcrumbs() {
  const published = useContext(BreadcrumbSegmentsContext);
  const segments = published.length > 0 ? published : [{ label: strings.reviews }];
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {segments.map((segment, index) => {
          const last = index === segments.length - 1;
          return (
            <Fragment key={`${segment.label}-${index}`}>
              <BreadcrumbItem>
                {segment.to !== undefined && !last ? (
                  <BreadcrumbLink asChild>
                    <Link to={segment.to}>{segment.label}</Link>
                  </BreadcrumbLink>
                ) : (
                  <BreadcrumbPage className="max-w-[28rem] truncate">
                    {segment.label}
                  </BreadcrumbPage>
                )}
              </BreadcrumbItem>
              {last ? null : <BreadcrumbSeparator />}
            </Fragment>
          );
        })}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
