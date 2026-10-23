import { Icon } from "../components/Icon";
import { useModal } from "../components/Modal";
import { setupStep, useOverview } from "../overview";
import { Wizard } from "../setup/Wizard";

const TEXT = [
  "",
  "Setup step 1 of 3: connect Cloudflare so mail can reach this server.",
  "Setup step 2 of 3: enable Tailscale so Outlook and Gmail can connect.",
  "Setup step 3 of 3: create your first mail account.",
];

export function SetupBanner() {
  const { ov } = useOverview();
  const { open } = useModal();
  const step = setupStep(ov);
  if (!step) return null;
  return (
    <div className="banner">
      <Icon name="alert" />
      <span>{TEXT[step]}</span>
      <button className="btn sm primary" onClick={() => open(<Wizard />)}>Continue setup</button>
    </div>
  );
}
