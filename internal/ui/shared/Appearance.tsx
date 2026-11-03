import { useState } from "react";
import { Dropdown, type Option } from "./Dropdown";
import { loadLook, saveLook, type Look } from "./prefs";
import { Card, Row } from "./ui";

const THEMES: Option<string>[] = [
  { value: "system", label: "System", hint: "Follow this computer", icon: "brightness_auto" },
  { value: "light", label: "Light", icon: "light_mode" },
  { value: "dark", label: "Dark", icon: "dark_mode" },
];
const MOTION: Option<string>[] = [
  { value: "system", label: "System", hint: "Follow this computer's setting" },
  { value: "reduce", label: "Reduced", hint: "No sliding or fading" },
];
const DENSITY: Option<string>[] = [
  { value: "system", label: "Comfortable", hint: "Roomy rows" },
  { value: "compact", label: "Compact", hint: "More rows on screen" },
];

// Appearance is how a dashboard looks, kept in this browser.
export function Appearance() {
  const [look, setLook] = useState(() => ({ theme: loadLook("theme"), motion: loadLook("motion"), density: loadLook("density") }));
  const set = (key: Look) => (value: string) => {
    saveLook(key, value);
    setLook((l) => ({ ...l, [key]: value }));
  };
  return (
    <Card title="Look and feel" sub="Kept in this browser.">
      <Row title="Theme" sub="Light, dark, or whatever this computer is set to">
        <Dropdown label="Theme" value={look.theme} options={THEMES} onChange={set("theme")} />
      </Row>
      <Row title="Motion" sub="Animations as pages change">
        <Dropdown label="Motion" value={look.motion} options={MOTION} onChange={set("motion")} />
      </Row>
      <Row title="Density" sub="How tightly lists are packed">
        <Dropdown label="Density" value={look.density} options={DENSITY} onChange={set("density")} />
      </Row>
    </Card>
  );
}
