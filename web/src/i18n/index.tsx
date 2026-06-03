import { createContext, useContext, useMemo } from "react";
import type { ReactNode } from "react";
import { usePreferencesStore } from "@/stores/preferences-store";
import { enUS } from "./locales/en_US";
import { zhCN } from "./locales/zh_CN";
import type { Dictionary, LocaleCode } from "./types";

export type { Dictionary, LocaleCode } from "./types";

const dictionaries: Record<LocaleCode, Dictionary> = {
  zh_CN: zhCN,
  en_US: enUS,
};

interface I18nValue {
  localeCode: LocaleCode;
  t: (key: string, fallback?: string) => string;
}

const I18nContext = createContext<I18nValue>({
  localeCode: "zh_CN",
  t: (_key, fallback = "") => fallback,
});

export function I18nProvider({ children }: { children: ReactNode }) {
  const localeCode = usePreferencesStore((state) => state.localeCode);
  const value = useMemo<I18nValue>(
    () => ({
      localeCode,
      t: (key, fallback = key) => dictionaries[localeCode][key] || fallback,
    }),
    [localeCode],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  return useContext(I18nContext);
}

export function translate(
  localeCode: LocaleCode,
  key: string,
  fallback: string,
) {
  return dictionaries[localeCode][key] || fallback;
}
