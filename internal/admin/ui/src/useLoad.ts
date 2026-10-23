import { useCallback, useEffect, useState } from "react";

// useLoad fetches data for a view and exposes a reload for after edits.
export function useLoad<T>(fetcher: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const reload = useCallback(async () => {
    try {
      setData(await fetcher());
      setError("");
    } catch (err) {
      setError((err as Error).message);
    }
    // fetcher is a stable module-level API call.
  }, []);
  useEffect(() => { reload(); }, [reload]);
  return { data, error, reload };
}
