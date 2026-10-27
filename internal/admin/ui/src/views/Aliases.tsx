import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Confirm } from "../components/Confirm";
import { DomainField } from "../components/DomainField";
import { MoreMenu } from "../components/Floating";
import { Icon } from "../components/Icon";
import { EmptyRow, PageHead, Panel } from "../components/Layout";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useCopy, useToast } from "../components/Toasts";
import { useOverview } from "../overview";
import type { Aliases as AliasMap } from "../types";
import { useLoad } from "../useLoad";

export function Aliases() {
  const { ov } = useOverview();
  const domains = ov?.domains ?? [];
  const domain = ov?.domain ?? "";
  const aliases = useLoad(api.aliases);
  const accounts = useLoad(api.accounts);
  const { open } = useModal();
  const copy = useCopy();
  const toast = useToast();

  const map = aliases.data ?? {};
  const emails = (accounts.data ?? []).map((a) => a.email);

  const save = async (next: AliasMap) => {
    await api.saveAliases(next);
    toast("Aliases saved");
    await aliases.reload();
  };

  const setCatchAll = (d: string, on: boolean, target: string) => {
    const next = { ...map };
    delete next["@" + d];
    if (on && target) next["@" + d] = target;
    save(next).catch((e) => toast((e as Error).message, true));
  };

  const edit = (alias = "", target = "") =>
    open(<EditAlias alias={alias} target={target} domains={domains} emails={emails} map={map} save={save} />);

  const rows = Object.entries(map).filter(([k]) => !k.startsWith("@")).sort(([a], [b]) => a.localeCompare(b));

  return (
    <>
      <PageHead title="Aliases" sub="Extra addresses that deliver to an account">
        <button className="btn primary" disabled={!emails.length} onClick={() => edit()}><Icon name="plus" />Add alias</button>
      </PageHead>
      <Panel>
        <div className="panel-body">
          {domains.map((d) => {
            const catchTarget = map["@" + d] ?? "";
            return (
              <div className="switch-row" key={d}>
                <label className="switch">
                  <input type="checkbox" checked={!!catchTarget} disabled={!emails.length}
                    onChange={(e) => setCatchAll(d, e.target.checked, catchTarget || emails[0])} />
                  <span />
                </label>
                <div className="txt">
                  <b>Catch-all{domains.length > 1 ? " for " + d : ""}</b>
                  <span className="muted">Deliver mail for any unknown @{d} address to </span>
                  <select value={catchTarget || emails[0] || ""} disabled={!emails.length}
                    onChange={(e) => catchTarget && setCatchAll(d, true, e.target.value)}>
                    {emails.map((e) => <option key={e} value={e}>{e}</option>)}
                  </select>
                </div>
              </div>
            );
          })}
          <div className="switch-row">
            <Icon name="info" />
            <div className="txt muted">
              Plus-addressing always works: <code>name+anything@{domain}</code> delivers to <code>name@</code>
            </div>
          </div>
        </div>
      </Panel>
      <Panel>
        <div className="table-wrap">
          <table>
            <thead><tr><th>Alias</th><th>Delivers to</th><th /></tr></thead>
            <tbody>
              {aliases.error ? <EmptyRow cols={3}>{aliases.error}</EmptyRow>
                : !rows.length ? <EmptyRow cols={3}>{emails.length ? "No aliases yet." : "Create an account first."}</EmptyRow>
                : rows.map(([k, v]) => (
                  <tr key={k}>
                    <td><span className="user-cell"><Icon name="at" />{k}</span></td>
                    <td>{v}</td>
                    <td className="act">
                      <MoreMenu items={() => [
                        { label: "Edit", icon: "edit", onClick: () => edit(k, v) },
                        { label: "Copy alias", icon: "copy", onClick: () => copy(k, "Alias copied") },
                        "-",
                        {
                          label: "Delete", icon: "trash", danger: true, onClick: () => open(
                            <Confirm title="Delete alias" label="Delete" danger message={`Stop accepting mail for ${k}?`}
                              onYes={async () => {
                                const next = { ...map };
                                delete next[k];
                                await save(next);
                              }} />),
                        },
                      ]} />
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}

interface EditProps {
  alias: string;
  target: string;
  domains: string[];
  emails: string[];
  map: AliasMap;
  save: (next: AliasMap) => Promise<void>;
}

function EditAlias({ alias, target, domains, emails, map, save }: EditProps) {
  const { close } = useModal();
  const [local, setLocal] = useState(alias.split("@")[0]);
  const [domain, setDomain] = useState(alias.split("@")[1] || domains[0] || "");
  const [to, setTo] = useState(target || emails[0] || "");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  const submit = async () => {
    const next = { ...map };
    if (alias) delete next[alias];
    next[local.trim().toLowerCase() + "@" + domain] = to;
    await save(next);
    close();
  };
  return (
    <Modal title={alias ? "Edit alias" : "Add alias"}
      footer={<><CloseButton label="Cancel" /><AsyncButton className="btn primary" onClick={submit}>Save</AsyncButton></>}>
      <div className="field">
        <label htmlFor="alias-local">Alias</label>
        <div className="input-suffix">
          <input id="alias-local" ref={input} type="text" placeholder="hello" spellCheck={false} value={local} onChange={(e) => setLocal(e.target.value)} />
          <span>@{domain}</span>
        </div>
      </div>
      <DomainField domains={domains} value={domain} onChange={setDomain} />
      <div className="field">
        <label htmlFor="alias-to">Delivers to</label>
        <select id="alias-to" value={to} onChange={(e) => setTo(e.target.value)}>
          {emails.map((e) => <option key={e} value={e}>{e}</option>)}
        </select>
      </div>
    </Modal>
  );
}
