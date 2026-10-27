// DomainField picks which domain an address is on; hidden with only one.
export function DomainField({ domains, value, onChange }: { domains: string[]; value: string; onChange: (d: string) => void }) {
  if (domains.length < 2) return null;
  return (
    <div className="field">
      <label htmlFor="addr-domain">Domain</label>
      <select id="addr-domain" value={value} onChange={(e) => onChange(e.target.value)}>
        {domains.map((d) => <option key={d} value={d}>{d}</option>)}
      </select>
    </div>
  );
}
