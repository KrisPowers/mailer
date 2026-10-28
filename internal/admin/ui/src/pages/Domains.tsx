import { Fragment, useEffect, useState } from "react";
import { api } from "../api";
import { Repair } from "../flows/Repair";
import { Setup } from "../flows/Setup";
import { fmtTime, plural } from "../format";
import { domainOK, useOverview, type Health } from "../overview";
import type { ConnectResult, DomainInfo } from "../types";
import { Button, IconButton } from "../ui/Button";
import { Field, SearchField } from "../ui/Field";
import { Icon } from "../ui/Icon";
import { Confirm, Modal, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, Metrics, PageHeader, Showing, Steps, Toolbar } from "../ui/Page";
import { Select } from "../ui/Select";
import { Toggle } from "../ui/Toggle";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

const RECORDS = ["MX", "SPF", "DKIM", "DMARC"];

function health(d: DomainInfo): Health {
  return !d.status ? "none" : domainOK(d.status) ? "ok" : "bad";
}

export function Domains() {
  const { ov, reload: reloadOverview } = useOverview();
  const { data, error, reload } = useLoad(api.domains);
  const { open } = useModal();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [problemsOnly, setProblemsOnly] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

  // The overview's refresh carries the latest checks.
  useEffect(() => { reload(); }, [ov]);

  const all = data ?? [];
  const list = all.filter((d) => d.name.includes(q.toLowerCase()) && (!problemsOnly || health(d) === "bad"));
  const refresh = async () => { await Promise.all([reload(), reloadOverview()]); };

  const remove = (d: DomainInfo) => open(
    <Confirm title="Remove domain" label="Remove"
      message={<>Stop receiving mail for <b>{d.name}</b>? Cloudflare stops passing its mail here and its aliases are deleted.
        DNS records stay, and {plural(d.accounts, "account")} on it keep their stored mail.</>}
      onYes={async () => {
        const r = await api.removeDomain(d.name);
        toast(r.warning ? "Removed. " + r.warning : "Removed " + d.name, r.warning ? "bad" : "ok");
        await refresh();
      }} />);

  return (
    <>
      <PageHeader title="Domains" actions={<>
        <Button icon="build" disabled={!all.length} onClick={() => open(<Repair domains={all.map((d) => d.name)} onDone={reload} />)}>Repair all</Button>
        <Button variant="primary" icon="add" onClick={() => open(ov?.cloudflare_setup ? <AddDomain onAdded={refresh} /> : <Setup step={1} />)}>Add domain</Button>
      </>} />
      <Metrics items={[
        { label: "Domains", value: all.length },
        { label: "Healthy", value: all.filter((d) => health(d) === "ok").length },
        { label: "Need attention", value: all.filter((d) => health(d) === "bad").length },
      ]} />
      <Toolbar>
        <SearchField value={q} onChange={setQ} />
        <Toggle checked={problemsOnly} onChange={setProblemsOnly}>Only show problems</Toggle>
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr><th className="col-expand" /><th className="col-dot" /><th>Name</th><th>Records</th><th className="num">Accounts</th><th>Connected</th><th className="col-actions">Actions</th></tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={7} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={7}>Loading…</EmptyRow>
              : !list.length ? <EmptyRow cols={7} icon="language">{all.length ? "No domains match." : "No domains yet. Add one to start receiving mail."}</EmptyRow>
              : list.map((d) => (
                <Fragment key={d.name}>
                  <tr>
                    <td className="col-expand">
                      <IconButton icon={expanded === d.name ? "expand_more" : "chevron_right"} label={expanded === d.name ? "Hide checks" : "Show checks"} tone="accent"
                        onClick={() => setExpanded(expanded === d.name ? null : d.name)} />
                    </td>
                    <td className="col-dot"><Dot health={health(d)} /></td>
                    <td className="strong">{d.name}{d.primary && <Chip health="plain">Primary</Chip>}</td>
                    <td>
                      <span className="chips">
                        {RECORDS.map((name) => {
                          const c = d.status?.checks.find((x) => x.name === name);
                          return <Chip key={name} health={!c ? "none" : c.ok ? "ok" : "bad"}>{name}</Chip>;
                        })}
                      </span>
                    </td>
                    <td className="num">{d.accounts}</td>
                    <td className="muted">{fmtTime(d.connected_at)}</td>
                    <td className="col-actions">
                      <span className="row-actions">
                        <IconButton icon="build" label="Repair" tone="accent" onClick={() => open(<Repair domains={[d.name]} onDone={reload} />)} />
                        <a className="icon-btn" href={`https://dash.cloudflare.com/${d.account_id}/${d.name}`} target="_blank" rel="noopener noreferrer"
                          aria-label="Open in Cloudflare" data-tip="Open in Cloudflare"><Icon name="open_in_new" /></a>
                        <span className="divider" />
                        <IconButton icon="delete" label={d.primary ? "The primary domain can't be removed" : "Remove domain"} tone="danger"
                          disabled={d.primary} onClick={() => remove(d)} />
                      </span>
                    </td>
                  </tr>
                  {expanded === d.name && (
                    <tr className="child-row">
                      <td className="col-expand" /><td className="col-dot" />
                      <td colSpan={5}>
                        {d.status ? <Steps steps={d.status.checks} error={d.status.error} /> : <p className="muted">Checks run every minute.</p>}
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
          </tbody>
        </table>
      </div>
      {data && <Showing count={list.length} />}
    </>
  );
}

function AddDomain({ onAdded }: { onAdded: () => Promise<void> }) {
  const { open, close } = useModal();
  const toast = useToast();
  const zones = useLoad(api.zones);
  const [zone, setZone] = useState("");
  const [result, setResult] = useState<ConnectResult | null>(null);
  const free = (zones.data ?? []).filter((z) => !z.connected).map((z) => z.name);
  const chosen = zone || free[0] || "";

  return (
    <Modal title="Add domain" actions={<>
      <Button variant="text" onClick={close}>Close</Button>
      {!!free.length && !result?.steps?.every((s) => s.ok) && (
        <Button variant="primary" onClick={async () => {
          setResult(null);
          const r = await api.connectCloudflare("", chosen);
          setResult(r);
          if (!r.error) {
            toast("Connected " + chosen);
            await onAdded();
          }
        }}>Connect</Button>
      )}
    </>}>
      {zones.error ? <p className="muted">{zones.error}</p>
        : !zones.data ? <p className="muted"><span className="spinner" /> Loading the domains on your Cloudflare account…</p>
        : !free.length ? <>
          <p>Every domain on your Cloudflare account is already connected.</p>
          <p className="muted">Add the domain to Cloudflare first, or sign in with the account that has it.</p>
          <Button icon="login" onClick={() => open(<Setup step={1} />)}>Sign in again</Button>
        </> : <>
          <p>Cloudflare receives the domain's mail and passes it here; Email Sending, DKIM, DMARC and SPF are set up too.</p>
          <Field id="ad-zone" label="Domain"><Select id="ad-zone" value={chosen} options={free} onChange={setZone} /></Field>
        </>}
      {result && <Steps steps={result.steps} error={result.error} />}
    </Modal>
  );
}
