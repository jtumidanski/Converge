import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { strings } from "@/lib/strings";

interface DiscardDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  pending?: boolean;
}

/** DiscardDialog guards the one irreversible action on the root page. */
export function DiscardDialog({
  open,
  onOpenChange,
  onConfirm,
  pending = false,
}: DiscardDialogProps) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{strings.discardReview}?</AlertDialogTitle>
          <AlertDialogDescription>{strings.discardConfirmSuffix}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{strings.cancel}</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} disabled={pending}>
            {strings.discardReview}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
