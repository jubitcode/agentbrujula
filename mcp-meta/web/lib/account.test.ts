/**
 * Signing out, and the bug this file exists because of.
 *
 * Three places said "you are signed in": the session cookie, the JWT in
 * `sessionStorage`, and `wallet_public_key` in `localStorage`. Sign-out
 * cleared the first two. The landing read the third, decided the visitor
 * was authenticated, and sent them into the app — which had no cookie, so
 * it sent them to sign-up. There was no way back: the link home read the
 * same stale key again.
 *
 * So the test is not "does sign-out work". It is "does sign-out leave
 * anything behind", and it checks each source by name.
 */

const mockClearSessionToken = jest.fn()
const mockDisconnect = jest.fn().mockResolvedValue(undefined)

jest.mock("./wallet-auth", () => ({
  clearSessionToken: () => mockClearSessionToken(),
  walletAuthFetch: jest.fn(),
}))

jest.mock("./wallet", () => ({
  disconnect: () => mockDisconnect(),
}))

import { signOut } from "./account"

const WALLET_KEY = "wallet_public_key"

describe("signOut", () => {
  beforeEach(() => {
    jest.clearAllMocks()
    global.fetch = jest.fn().mockResolvedValue({ ok: true, json: async () => ({}) }) as never
    localStorage.setItem(WALLET_KEY, "GSOMEWALLET234567ABCDEFGHIJKLMNOPQRSTUVWXYZ2345678")
  })

  test("expires the session cookie on the server", async () => {
    await signOut()
    expect(global.fetch).toHaveBeenCalledWith("/api/session", { method: "DELETE" })
  })

  test("clears the cached JWT", async () => {
    await signOut()
    expect(mockClearSessionToken).toHaveBeenCalled()
  })

  /** The one that was missing, and the whole reason for the loop. */
  test("clears the saved wallet from localStorage", async () => {
    await signOut()
    expect(localStorage.getItem(WALLET_KEY)).toBeNull()
  })

  test("disconnects the wallet kit", async () => {
    await signOut()
    expect(mockDisconnect).toHaveBeenCalled()
  })

  /**
   * A sign-out that half-works and looks complete is worse than one that
   * fails loudly: the person believes they are out. Every source is cleared
   * even when an earlier step throws.
   */
  test("still clears local state when the server call fails", async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error("network down")) as never
    await signOut()
    expect(mockClearSessionToken).toHaveBeenCalled()
    expect(localStorage.getItem(WALLET_KEY)).toBeNull()
  })

  test("still clears the wallet when the wallet kit refuses", async () => {
    mockDisconnect.mockRejectedValueOnce(new Error("kit exploded"))
    await signOut()
    expect(localStorage.getItem(WALLET_KEY)).toBeNull()
  })

  test("does not throw when nothing was stored", async () => {
    localStorage.clear()
    await expect(signOut()).resolves.toBeUndefined()
  })
})
