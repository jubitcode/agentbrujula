/**
 * The home page is the agent catalogue.
 *
 * This file used to test a landing page — hero, a five-step timeline, a
 * "Built by" section, two Connect Wallet buttons — and had been failing since
 * that landing was replaced by the catalogue. The sixteen assertions below it
 * describe a screen that no longer exists, so they are replaced rather than
 * patched.
 *
 * Text is read from the dictionary instead of written out, so these keep
 * passing in either language and do not break on a wording change.
 */

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { es } from "@/lib/i18n/es";

jest.mock("next/navigation", () => ({
  useRouter: () => ({ push: jest.fn() }),
  usePathname: () => "/",
}));

jest.mock("@/lib/api", () => ({
  getAgentCard: jest.fn(),
}));

jest.mock("next/link", () => {
  return function MockLink({ href, children, ...props }: any) {
    return <a href={href} {...props}>{children}</a>;
  };
});

import HomePage from "./page";
import { getAgentCard } from "@/lib/api";

const mockCard = getAgentCard as jest.MockedFunction<typeof getAgentCard>;

const SKILLS = [
  {
    id: "pm-planner",
    name: "PM Planner",
    description: "Turns an idea into a plan",
    tags: ["pm", "planificación"],
    examples: ["Una app de barrio"],
  },
  {
    id: "deep-research",
    name: "Deep Research",
    description: "Researches a topic in depth",
    tags: ["research", "web"],
    examples: [],
  },
  // Left over from a test run. The catalogue must not show it.
  { id: "test-tool-1792540296", name: "Leftover", description: "x", tags: [], examples: [] },
];

beforeEach(() => {
  jest.clearAllMocks();
});

describe("Catalogue", () => {
  test("shows a loading state before the registry answers", () => {
    mockCard.mockReturnValue(new Promise(() => {}) as any);
    render(<HomePage />);
    expect(screen.getByText(es["home.loading"] as string)).toBeInTheDocument();
  });

  test("lists the agents the registry returns, and hides test leftovers", async () => {
    mockCard.mockResolvedValue({ skills: SKILLS } as any);
    render(<HomePage />);

    await waitFor(() => {
      expect(screen.getByText("PM Planner")).toBeInTheDocument();
    });
    expect(screen.getByText("Deep Research")).toBeInTheDocument();
    expect(screen.queryByText("Leftover")).toBeNull();
  });

  test("the PM is featured and links to its own screen", async () => {
    mockCard.mockResolvedValue({ skills: SKILLS } as any);
    render(<HomePage />);

    await waitFor(() => {
      expect(screen.getByText(es["home.startHere"] as string)).toBeInTheDocument();
    });
    const featured = screen.getByText("PM Planner").closest("a");
    expect(featured).toHaveAttribute("href", "/pm");
  });

  /**
   * The filter row showed one chip per tag — 25 of them. These are groups,
   * and only the ones that match something in the registry are offered.
   */
  test("tags are offered as groups, and only groups that match", async () => {
    mockCard.mockResolvedValue({ skills: SKILLS } as any);
    render(<HomePage />);

    await waitFor(() => {
      expect(screen.getByText(es["home.group.planning"] as string)).toBeInTheDocument();
    });
    expect(screen.getByText(es["home.group.research"] as string)).toBeInTheDocument();
    // Nothing in the registry is tagged for these two.
    expect(screen.queryByText(es["home.group.writing"] as string)).toBeNull();
    expect(screen.queryByText(es["home.group.technical"] as string)).toBeNull();
  });

  test("picking a group filters the list", async () => {
    const user = userEvent.setup();
    mockCard.mockResolvedValue({ skills: SKILLS } as any);
    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("Deep Research")).toBeInTheDocument());
    await user.click(screen.getByText(es["home.group.research"] as string));

    expect(screen.getByText("Deep Research")).toBeInTheDocument();
    expect(screen.queryByText("PM Planner")).toBeNull();
  });

  /** Grouping replaced the chips; it must not have removed the tags. */
  test("an individual tag still works as a search term", async () => {
    const user = userEvent.setup();
    mockCard.mockResolvedValue({ skills: SKILLS } as any);
    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("PM Planner")).toBeInTheDocument());
    await user.type(screen.getByLabelText(es["home.searchLabel"] as string), "web");

    expect(screen.getByText("Deep Research")).toBeInTheDocument();
    expect(screen.queryByText("PM Planner")).toBeNull();
  });

  test("a search that matches nothing says so", async () => {
    const user = userEvent.setup();
    mockCard.mockResolvedValue({ skills: SKILLS } as any);
    render(<HomePage />);

    await waitFor(() => expect(screen.getByText("PM Planner")).toBeInTheDocument());
    await user.type(screen.getByLabelText(es["home.searchLabel"] as string), "zzzz");

    expect(screen.getByText(es["home.empty"] as string)).toBeInTheDocument();
  });

  /**
   * A registry that cannot be reached is not an empty registry, and the two
   * must not look the same — that confusion is what `/explore` shipped.
   */
  test("a failed registry read is an error, not an empty catalogue", async () => {
    mockCard.mockRejectedValue(new Error("connection refused"));
    render(<HomePage />);

    await waitFor(() => {
      expect(screen.getByText(es["home.error"] as string)).toBeInTheDocument();
    });
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(screen.queryByText(es["home.empty"] as string)).toBeNull();
    expect(screen.queryByText(es["home.loading"] as string)).toBeNull();
  });
});
