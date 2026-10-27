import { useEffect, useState } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Badge, type Health } from "../components/Badge";
import { Confirm } from "../components/Confirm";
import { MoreMenu, type MenuItem } from "../components/Floating";
import { Icon } from "../components/Icon";
import { EmptyRow, PageHead, Panel } from "../components/Layout";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { fmtTime, plural } from "../format";
import { domainOK, useOverview } from "../overview";
import { Progress } from "../setup/Progress";
import { RepairCloudflare } from "../setup/RepairCloudflare";
import { Wizard } from "../setup/Wizard";
import type { ConnectResult, DomainInfo, Zone } from "../types";
import { useLoad } from "../useLoad";

// The DNS records each domain needs, shown as badges in the table.
const RECORDS = ["MX", "SPF", "DKIM", "DMARC"];

function health(d: DomainInfo): Health {
  if (!d.status) return "none";
  return domainOK(d.status) ? "ok" : "bad";
}

export function Domains() {
  const { ov, reload: reloadOverview } = useOverview();
  const { data, error, reload } = useLoad(api.domains);
  const { open } = useModal();
  const toast = useToast();

  // Follow the overview's refresh, which carries the latest checks.
  useEffect(() => {
    reload();
  }, [ov]);

  const refresh = async () => {
    await Promise.all([reload(), reloadOverview()]);
  };

  const menu = (d: DomainInfo) => (): MenuItem[] => [
    { label: "View checks", icon: "activity", onClick: () => open(<DomainDetails domain={d} />) },
    { label: "Repair", icon: "refresh", onClick: () => open(<RepairCloudflare domains={[d.name]} onDone={reload} />) },
    {
      label: "Open in Cloudflare", icon: "external",
      onClick: () => api.openURL(`https://dash.cloudflare.com/${d.account_id}/${d.name}`),
    },
    ...(d.primary ? [] : ["-" as const, {
      label: "Remove", icon: "trash" as const, danger: true, onClick: () => open(
        <Confirm title="Remove domain" label="Remove" danger
          message={`Stop receiving mail for ${d.name}? Cloudflare stops passing its mail here and its aliases are deleted. `
            + `DNS records stay, and ${plural(d.accounts, "account")} on it keep their stored mail.`}
          onYes={async () => {
            const r = await api.removeDomain(d.name);
            toast(r.warning ? "Removed. " + r.warning : "Removed " + d.name, !!r.warning);
            await refresh();
          }} />),
    }]),
  ];

  const list = data ?? [];
  return (
    <>
      <PageHead title="Domains" sub={data ? plural(list.length, "domain") : ""}>
        <button className="btn" disabled={!list.length} onClick={() => open(<RepairCloudflare domains={list.map((d) => d.name)} onDone={reload} />)}>
          <Icon name="refresh" />Repair all
        </button>
        <button className="btn primary" onClick={() => open(ov?.cloudflare_setup ? <AddDomain onAdded={refresh} /> : <Wizard step={1} />)}>
          <Icon name="plus" />Add domain
        </button>
      </PageHead>
      <Panel>
        <div className="table-wrap">
          <table>
            <thead>
              <tr><th>Domain</th><th>Records</th><th className="num">Accounts</th><th>Connected</th><th>Status</th><th /></tr>
            </thead>
            <tbody>
              {error ? <EmptyRow cols={6}>{error}</EmptyRow>
                : !data ? <EmptyRow cols={6}>Loading…</EmptyRow>
                : !list.length ? <EmptyRow cols={6}>No domains yet. Add one to start receiving mail.</EmptyRow>
                : list.map((d) => (
                  <tr key={d.name} className="clickable" onClick={() => open(<DomainDetails domain={d} />)}>
                    <td>
                      <span className="user-cell">
                        <Icon name="globe" /><b>{d.name}</b>
                        {d.primary && <Badge kind="plain">Primary</Badge>}
                      </span>
                    </td>
                    <td>
                      <span className="badges">
                        {RECORDS.map((name) => {
                          const c = d.status?.checks.find((x) => x.name === name);
                          return <Badge key={name} kind={!c ? "none" : c.ok ? "ok" : "bad"}>{name}</Badge>;
                        })}
                      </span>
                    </td>
                    <td className="num">{d.accounts}</td>
                    <td className="muted">{fmtTime(d.connected_at)}</td>
                    <td><Badge kind={health(d)}>{!d.status ? "Checking" : domainOK(d.status) ? "Healthy" : "Problem"}</Badge></td>
                    <td className="act" onClick={(e) => e.stopPropagation()}><MoreMenu items={menu(d)} /></td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}

function DomainDetails({ domain: d }: { domain: DomainInfo }) {
  const { open } = useModal();
  return (
    <Modal title={d.name} wide footer={<>
      <CloseButton />
      <button className="btn primary" onClick={() => open(<RepairCloudflare domains={[d.name]} />)}><Icon name="refresh" />Repair</button>
    </>}>
      {d.status
        ? <Progress steps={d.status.checks} error={d.status.error} />
        : <p className="muted">Checks run every minute; the first results will appear shortly.</p>}
      <p className="muted small">
        Read from Cloudflare's API. Repair re-applies Email Routing, the catch-all rule, DKIM, DMARC and SPF for this domain.
      </p>
    </Modal>
  );
}

function AddDomain({ onAdded }: { onAdded: () => Promise<void> }) {
  const { open } = useModal();
  const toast = useToast();
  const zones = useLoad(api.zones);
  const [zone, setZone] = useState("");
  const [result, setResult] = useState<ConnectResult | null>(null);

  const free = (zones.data ?? []).filter((z: Zone) => !z.connected);
  const chosen = zone || free[0]?.name || "";

  const connect = async () => {
    setResult(null);
    const r = await api.connectCloudflare("", chosen);
    setResult(r);
    if (!r.error) {
      toast("Connected " + chosen);
      await onAdded();
    }
  };

  return (
    <Modal title="Add domain" footer={<>
      <CloseButton />
      {!!free.length && !result?.steps?.every((s) => s.ok) &&
        <AsyncButton className="btn primary" onClick={connect}>Connect</AsyncButton>}
    </>}>
      {zones.error ? <p className="muted">{zones.error}</p>
        : !zones.data ? <p className="muted">Loading the domains your Cloudflare token can see…</p>
        : !free.length ? <>
          <p>Every domain this token can see is already connected.</p>
          <p className="muted">
            To add another, give the token access to it under Zone Resources
            {" "}(<a href="https://dash.cloudflare.com/profile/api-tokens" target="_blank" rel="noopener">API tokens</a>),
            or use a different token.
          </p>
          <button className="btn sm" onClick={() => open(<Wizard step={1} />)}><Icon name="key" />Use a different token</button>
        </> : <>
          <p>Cloudflare will receive mail for the domain and pass it to this server, and the DKIM, DMARC and SPF records are published.</p>
          <div className="field">
            <label htmlFor="zone">Domain</label>
            <select id="zone" value={chosen} onChange={(e) => setZone(e.target.value)}>
              {free.map((z) => <option key={z.name} value={z.name}>{z.name}</option>)}
            </select>
          </div>
        </>}
      {result && <Progress steps={result.steps} error={result.error} />}
    </Modal>
  );
}
