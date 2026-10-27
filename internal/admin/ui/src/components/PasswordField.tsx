// PasswordField is where the mail app password is chosen. It stays visible
// so it can be read back while typing it into Outlook or the Gmail app.
export function PasswordField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="field">
      <label htmlFor="acct-pw">Password</label>
      <input id="acct-pw" type="text" autoComplete="off" spellCheck={false} placeholder="At least 10 characters"
        value={value} onChange={(e) => onChange(e.target.value)} />
      <div className="hint">You'll sign in to Outlook or the Gmail app with it. Leave empty to generate one.</div>
    </div>
  );
}
