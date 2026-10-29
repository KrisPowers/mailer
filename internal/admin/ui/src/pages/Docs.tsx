import { useEffect } from "react";
import configuration from "../../../../../docs/configuration.md?raw";
import dashboard from "../../../../../docs/dashboard.md?raw";
import howItWorks from "../../../../../docs/how-it-works.md?raw";
import troubleshooting from "../../../../../docs/troubleshooting.md?raw";
import { href } from "../router";
import { Markdown } from "../ui/Markdown";
import { PageHeader, Tabs } from "../ui/Page";

// The guides in docs/, the same files the repository shows, built into
// the dashboard. param is "<guide>" or "<guide>/<heading id>".
const GUIDES = {
  "how-it-works": ["How it works", howItWorks],
  configuration: ["Configuration", configuration],
  dashboard: ["Dashboard", dashboard],
  troubleshooting: ["Troubleshooting", troubleshooting],
} as const;
type Guide = keyof typeof GUIDES;

const isGuide = (g: string): g is Guide => g in GUIDES;

// guideHref turns a docs/ link such as "configuration.md#ports" into the
// dashboard address of that guide and heading.
function guideHref(target: string) {
  const [file, anchor] = target.split("#");
  const g = file.replace(/\.md$/, "");
  return href("docs", isGuide(g) ? g + (anchor ? "/doc-" + anchor : "") : "");
}

export function Docs({ param }: { param: string }) {
  const [g, section] = param.split("/");
  const guide: Guide = isGuide(g) ? g : "how-it-works";

  // After the page has scrolled to the top for the new address.
  useEffect(() => {
    if (!section) return;
    const f = requestAnimationFrame(() => document.getElementById(section)?.scrollIntoView());
    return () => cancelAnimationFrame(f);
  }, [guide, section]);

  return (
    <>
      <PageHeader title="Docs" />
      <Tabs<Guide> value={guide} onChange={(t) => { location.hash = href("docs", t); }}
        tabs={(Object.keys(GUIDES) as Guide[]).map((k): [Guide, string] => [k, GUIDES[k][0]])} />
      <Markdown key={guide} source={GUIDES[guide][1]} guide={guideHref} />
    </>
  );
}
