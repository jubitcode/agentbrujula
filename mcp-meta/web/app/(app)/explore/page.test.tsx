import { render, screen, waitFor } from "@testing-library/react";
import { es } from "@/lib/i18n/es";

jest.mock("@/lib/api", () => ({
  getAgentCard: jest.fn(),
}));

jest.mock("next/link", () => {
  return function MockLink({ href, children, ...props }: any) {
    return <a href={href} {...props}>{children}</a>;
  };
});

import ExplorePage from "./page";
import { getAgentCard } from "@/lib/api";

const mockGetAgentCard = getAgentCard as jest.MockedFunction<typeof getAgentCard>;

beforeEach(() => {
  jest.clearAllMocks();
});

describe("Explore page", () => {
  test("renders heading", async () => {
    mockGetAgentCard.mockResolvedValue({
      name: "test",
      description: "test",
      url: "",
      version: "",
      protocolVersion: "",
      provider: { organization: "" },
      capabilities: {},
      defaultInputModes: [],
      defaultOutputModes: [],
      skills: [],
    } as any);

    render(<ExplorePage />);
    // From the dictionary: the heading is Spanish by default now.
    expect(screen.getByText(es["explore.title"] as string)).toBeInTheDocument();
  });

  test("renders skills when loaded", async () => {
    mockGetAgentCard.mockResolvedValue({
      skills: [
        {
          id: "deep-research",
          name: "Deep Research",
          description: "Research a topic in depth",
          tags: ["research", "web"],
          examples: ["Research AI trends"],
        },
        {
          id: "summarize",
          name: "Summarize",
          description: "Summarize text into one sentence",
          tags: ["nlp"],
          examples: [],
        },
      ],
    } as any);

    render(<ExplorePage />);

    await waitFor(() => {
      expect(screen.getByText("Deep Research")).toBeInTheDocument();
    });
    expect(screen.getByText("Summarize")).toBeInTheDocument();
    expect(screen.getByText(/research a topic/i)).toBeInTheDocument();
  });

  test("renders tags as chips", async () => {
    mockGetAgentCard.mockResolvedValue({
      skills: [
        { id: "s1", name: "Skill", description: "desc", tags: ["research", "web"], examples: [] },
      ],
    } as any);

    render(<ExplorePage />);

    await waitFor(() => {
      expect(screen.getByText("research")).toBeInTheDocument();
    });
    expect(screen.getByText("web")).toBeInTheDocument();
  });

  test("each skill card has a Stellar Expert link to the Tool Registry contract", async () => {
    mockGetAgentCard.mockResolvedValue({
      skills: [
        { id: "deep-research", name: "Deep Research", description: "d", tags: [], examples: [] },
        { id: "summarize", name: "Summarize", description: "d", tags: [], examples: [] },
      ],
    } as any);

    render(<ExplorePage />);

    await waitFor(() => {
      expect(screen.getByText("Deep Research")).toBeInTheDocument();
    });

    const registryLinks = Array.from(document.querySelectorAll("a")).filter((a) =>
      (a.getAttribute("href") || "").includes(
        "stellar.expert/explorer/testnet/contract/CARJRAXMYOM7TNNCR2K4OVZTO6BYUTUIW5AB2ZHDSF5UZXBSWU7YZQBW"
      )
    );
    // One link per skill card
    expect(registryLinks.length).toBeGreaterThanOrEqual(2);
  });

  test("shows empty state when no skills", async () => {
    mockGetAgentCard.mockResolvedValue({ skills: [] } as any);

    render(<ExplorePage />);

    await waitFor(() => {
      expect(screen.getByText(es["explore.empty"] as string)).toBeInTheDocument();
    });
  });
});
