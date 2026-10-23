import type { ReactNode } from "react";
import { AsyncButton } from "./AsyncButton";
import { CloseButton, Modal, useModal } from "./Modal";

interface Props {
  title: string;
  message: ReactNode;
  label: string;
  danger?: boolean;
  // onYes runs after the dialog closes, so it can open a follow-up dialog.
  onYes: () => Promise<unknown> | unknown;
}

export function Confirm({ title, message, label, danger, onYes }: Props) {
  const { close } = useModal();
  return (
    <Modal
      title={title}
      footer={<>
        <CloseButton label="Cancel" />
        <AsyncButton className={"btn " + (danger ? "danger" : "primary")} onClick={async () => { close(); await onYes(); }}>
          {label}
        </AsyncButton>
      </>}
    >
      <p>{message}</p>
    </Modal>
  );
}
