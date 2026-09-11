import { Link, useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useCurrentUser, useLogout } from "@/lib/hooks/api/useAuth";

/**
 * AccountMenu is the hosted-mode account control: the signed-in username,
 * links to the two settings pages, and log out.
 *
 * It calls useCurrentUser itself rather than taking the user as a prop, which
 * 401s for an unauthenticated hosted visitor and lets AuthProvider's handler
 * redirect to /login. On /login that handler is a no-op, so this renders
 * nothing until a user resolves — there is no unauthenticated appearance for
 * it to have.
 */
export function AccountMenu() {
  const { data } = useCurrentUser();
  const logout = useLogout();
  const navigate = useNavigate();

  if (!data) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm">
          {data.username}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{data.username}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/settings/providers">Provider settings</Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild>
          <Link to="/settings/account">Account settings</Link>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            logout.mutate(undefined, {
              onSuccess: () => void navigate("/login"),
            });
          }}
        >
          Log out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
