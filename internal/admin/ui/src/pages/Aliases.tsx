import { useState } from "react";
import { api } from "../api";
import { useOverview } from "../overview";
import type { Aliases as AliasMap } from "../types";
import { Button, IconButton } from "../ui/Button";
import { Field, SearchField, TextInput } from "../ui/Field";
import { Icon } from "../ui/Icon";
import { Confirm, Modal, useModal } from "../ui/Modal";
import { Dot, EmptyRow, Metrics, PageHeader, Section, Showing, Toolbar } from "../ui/Page";
import { Select } from "../ui/Select";
import { Checkbox, Toggle } from "../ui/Toggle";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

export function Aliases() {
  const { ov } = useOverview();
  const domains = ov?.domains ?? [];
  const aliases = useLoad(api.aliases);
  const accounts = useLoad(api.accounts);
  const { open } = useModal();
  const copy = useCopy();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const map = aliases.data ?? {};
  const emails = (accounts.data ?? []).map((a) => a.email);
  const rows = Object.entries(map).filter(([k]) => !k.startsWith("@")).sort(([a], [b]) => a.localeCompare(b));
  const list = rows.filter(([k, v]) => (k + " " + v).includes(q.toLowerCase()));
  const allPicked = list.length > 0 && list.every(([k]) => picked.has(k));

  const save = async (next: AliasMap) => {
    await api.saveAliases(next);
    toast("Aliases saved");
    setPicked(new Set());
    await aliases.reload();
  };
  const setCatchAll = (d: string, target: string) => {
    const next = { ...map };
    delete next["@" + d];
    if (target) next["@" + d] = target;
    save(next).catch((e) => toast((e as Error).message, "bad"));
  };
  const edit = (alias = "", target = "") => open(<EditAlias alias={alias} target={target} domains={domains} emails={emails} map={map} save={save} />);
  const remove = (keys: string[]) => open(
    <Confirm title={keys.length === 1 ? "Delete alias" : "Delete " + keys.length + " aliases"} label="Delete"
      message={<>Stop accepting mail for {keys.length === 1 ? <b>{keys[0]}</b> : keys.length + " aliases"}?</>}
      onYes={async () => {
        const next = { ...map };
        keys.forEach((k) => delete next[k]);
        await save(next);
      }} />);

  return (
    <>
      <PageHeader title="Aliases" actions={<Button variant="primary" icon="add" disabled={!emails.length} onClick={() => edit()}>Add alias</Button>} />
      <Metrics items={[
        { label: "Aliases", value: rows.length },
        { label: "Catch-alls on", value: domains.filter((d) => map["@" + d]).length + " of " + domains.length },
      ]} />
      <Section title="Catch-all">
        <div className="settings-list">
          {domains.map((d) => {
            const target = map["@" + d] ?? "";
            return (
              <div className="setting" key={d}>
                <Toggle checked={!!target} disabled={!emails.length} onChange={(on) => setCatchAll(d, on ? emails[0] : "")} />
                <div className="setting-text">
                  <b>Catch-all for @{d}</b>
                  <span className="muted">Deliver mail sent to any address on {d} that doesn't exist to</span>
                </div>
                <Select value={target || emails[0] || ""} options={emails} disabled={!target} label={"Catch-all target for " + d}
                  onChange={(v) => setCatchAll(d, v)} />
              </div>
            );
          })}
          <div className="setting">
            <Icon name="info" />
            <div className="setting-text muted">Plus-addressing always works: <code>name+anything@{ov?.domain}</code> delivers to <code>name@{ov?.domain}</code>.</div>
          </div>
        </div>
      </Section>
      <Toolbar actions={picked.size > 0 && <Button variant="danger" icon="delete" onClick={() => remove([...picked])}>Delete {picked.size}</Button>}>
        <SearchField value={q} onChange={setQ} />
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th className="col-check">
                <Checkbox label="Select all" checked={allPicked} mixed={!allPicked && list.some(([k]) => picked.has(k))}
                  onChange={(on) => setPicked(on ? new Set(list.map(([k]) => k)) : new Set())} />
              </th>
              <th className="col-dot" /><th>Alias</th><th>Delivers to</th><th className="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {aliases.error ? <EmptyRow cols={5} icon="error">{aliases.error}</EmptyRow>
              : !aliases.data ? <EmptyRow cols={5}>Loading…</EmptyRow>
              : !list.length ? <EmptyRow cols={5} icon="alternate_email">{rows.length ? "No aliases match." : emails.length ? "No aliases yet." : "Create an account first."}</EmptyRow>
              : list.map(([k, v]) => (
                <tr key={k} className={picked.has(k) ? "selected" : ""}>
                  <td className="col-check">
                    <Checkbox label={"Select " + k} checked={picked.has(k)} onChange={(on) => {
                      const next = new Set(picked);
                      if (on) next.add(k); else next.delete(k);
                      setPicked(next);
                    }} />
                  </td>
                  <td className="col-dot"><Dot health={emails.includes(v) ? "ok" : "bad"} label={emails.includes(v) ? "Delivering" : "Target account is gone"} /></td>
                  <td className="strong">{k}</td>
                  <td>{v}</td>
                  <td className="col-actions">
                    <span className="row-actions">
                      <IconButton icon="edit" label="Edit" tone="accent" onClick={() => edit(k, v)} />
                      <IconButton icon="content_copy" label="Copy alias" onClick={() => copy(k, "Alias copied")} />
                      <span className="divider" />
                      <IconButton icon="delete" label="Delete alias" tone="danger" onClick={() => remove([k])} />
                    </span>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
      {aliases.data && <Showing count={list.length} />}
    </>
  );
}

function EditAlias({ alias, target, domains, emails, map, save }: {
  alias: string;
  target: string;
  domains: string[];
  emails: string[];
  map: AliasMap;
  save: (next: AliasMap) => Promise<void>;
}) {
  const { close } = useModal();
  const [local, setLocal] = useState(alias.split("@")[0]);
  const [domain, setDomain] = useState(alias.split("@")[1] || domains[0] || "");
  const [to, setTo] = useState(target || emails[0] || "");
  return (
    <Modal title={alias ? "Edit alias" : "Add alias"} actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" disabled={!local.trim()} onClick={async () => {
        const next = { ...map };
        if (alias) delete next[alias];
        next[local.trim().toLowerCase() + "@" + domain] = to;
        await save(next);
        close();
      }}>Save</Button>
    </>}>
      <Field id="ea-local" label="Alias"><TextInput id="ea-local" autoFocus placeholder="hello" value={local} onChange={setLocal} suffix={"@" + domain} /></Field>
      {domains.length > 1 && <Field id="ea-domain" label="Domain"><Select id="ea-domain" value={domain} options={domains} onChange={setDomain} /></Field>}
      <Field id="ea-to" label="Delivers to"><Select id="ea-to" value={to} options={emails} onChange={setTo} /></Field>
    </Modal>
  );
}
