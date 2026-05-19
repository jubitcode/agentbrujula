import { STELLAR_NETWORK, HORIZON_URL } from "./stellar-env";

describe("stellar-env", () => {
  test("defaults to TESTNET", () => {
    expect(STELLAR_NETWORK).toBe("TESTNET");
  });

  test("defaults to testnet horizon", () => {
    expect(HORIZON_URL).toContain("horizon");
  });
});
