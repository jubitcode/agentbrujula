"use client"

/**
 * The sentence that keeps this honest.
 *
 * Brújula's whole claim is that the money is held by a contract and released
 * against a check. A screen that shows that cycle without the chain
 * underneath is a useful thing to demonstrate and a dangerous thing to leave
 * ambiguous — someone could reasonably believe funds moved, and for a
 * product whose selling point is custody, that is the worst possible
 * misunderstanding.
 *
 * So it says what is simulated and what is not, in the same words every
 * time, and it is not dismissible.
 */

import { useI18n } from "@/lib/i18n"

export function DemoNotice({ short }: { short?: boolean }) {
  const { t } = useI18n()
  return (
    <div className="demo-banner" role="note">
      <span className="demo-badge">{t("demo.badge")}</span>
      <span>{short ? t("demo.bannerShort") : t("demo.banner")}</span>
    </div>
  )
}
