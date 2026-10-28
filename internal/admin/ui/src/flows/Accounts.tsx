import { useState } from "react";
import { api } from "../api";
import { setupStep, useOverview } from "../overview";
import { href } from "../router";
import { Button } from "../ui/Button";
import { Field, PasswordInput, TextInput } from "../ui/Field";
import { Icon } from "../ui/Icon";
import { Modal, useModal } from "../ui/Modal";
import { Select } from "../ui/Select";
import { useCopy } from "../ui/Toast";
import { Setup } from "./Setup";

// NewAccount creates a mailbox; the password is the one typed or a
// generated one, shown once afterwards.
export function NewAccount() {
  const { ov, reload } = useOverview();
  const { open, close } = useModal();
  const domains = ov?.domains ?? [];
  const [name, setName] = useState("");
  const [domain, setDomain] = useState(domains[0] ?? "");
  const [password, setPassword] = useState("");
  const create = async () => {
    const r = await api.createAccount(name.includes("@") ? name : name + "@" + domain, password);
    await reload();
    open(<PasswordShown title="Account created" email={r.email} password={r.password} />);
  };
  return (
    <Modal title="New account" actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" disabled={!name.trim()} onClick={create}>Create</Button>
    </>}>
      <Field id="na-name" label="Address">
        <TextInput id="na-name" autoFocus placeholder="name" value={name} onChange={setName} suffix={"@" + (domain || "…")} />
      </Field>
      {domains.length > 1 && (
        <Field id="na-domain" label="Domain"><Select id="na-domain" value={domain} options={domains} onChange={setDomain} /></Field>
      )}
      <Field id="na-pw" label="Password" hint="At least 10 characters. Leave empty to generate one.">
        <PasswordInput id="na-pw" value={password} onChange={setPassword} />
      </Field>
    </Modal>
  );
}

// SetPassword replaces an account's password; the old one stops working.
export function SetPassword({ email }: { email: string }) {
  const { open, close } = useModal();
  const [password, setPassword] = useState("");
  return (
    <Modal title={"New password for " + email} actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" onClick={async () => {
        const r = await api.setPassword(email, password);
        open(<PasswordShown title="Password set" email={r.email} password={r.password} />);
      }}>Set password</Button>
    </>}>
      <Field id="sp-pw" label="Password" hint="Leave empty to generate one. The current password stops working right away, in every app.">
        <PasswordInput id="sp-pw" value={password} onChange={setPassword} autoFocus />
      </Field>
    </Modal>
  );
}

// PasswordShown shows a password once; it's stored hashed afterwards.
export function PasswordShown({ title, email, password }: { title: string; email: string; password: string }) {
  const { ov, reload } = useOverview();
  const { open, close } = useModal();
  const copy = useCopy();
  return (
    <Modal title={title} actions={
      <Button variant="primary" onClick={async () => {
        const fresh = await reload();
        if (setupStep(fresh ?? ov)) open(<Setup />);
        else close();
      }}>Done</Button>
    }>
      <p>Password for <b>{email}</b></p>
      <div className="secret">
        <code>{password}</code>
        <button type="button" className="icon-btn" aria-label="Copy password" data-tip="Copy password" onClick={() => copy(password, "Password copied")}>
          <Icon name="content_copy" />
        </button>
      </div>
      <div className="callout warn"><Icon name="warning" /><span>This is the only time it's shown. Put it in your mail app now.</span></div>
      <p className="muted">Every setting a mail app needs is on the account's page: <a href={href("accounts", email)} onClick={close}>{email}</a>.</p>
    </Modal>
  );
}
