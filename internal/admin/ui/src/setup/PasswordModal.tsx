import { AsyncButton } from "../components/AsyncButton";
import { CopyButton } from "../components/CopyButton";
import { Icon } from "../components/Icon";
import { Modal, useModal } from "../components/Modal";
import { setupStep, useOverview } from "../overview";
import { ClientSettings } from "./ClientSettings";
import { Wizard } from "./Wizard";

// PasswordModal shows a generated password once, with the client settings
// needed to add the account to a mail app.
export function PasswordModal({ title, email, password }: { title: string; email: string; password: string }) {
  const { open, close } = useModal();
  const { reload } = useOverview();
  return (
    <Modal
      title={title}
      wide
      footer={
        <AsyncButton className="btn primary" onClick={async () => {
          const ov = await reload();
          if (setupStep(ov)) open(<Wizard />);
          else close();
        }}>Done</AsyncButton>
      }
    >
      <p>Password for <b>{email}</b>:</p>
      <div className="secret-box"><span>{password}</span><CopyButton text={password} label="Password copied" /></div>
      <div className="note"><Icon name="alert" /><span>This is the only time it's shown. Save it in your mail app now.</span></div>
      <h3 className="section-label">Client settings</h3>
      <ClientSettings />
    </Modal>
  );
}
