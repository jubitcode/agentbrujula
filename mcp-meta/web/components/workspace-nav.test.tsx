import { render, screen } from "@testing-library/react";

jest.mock("next/link", () => {
  return function MockLink({ href, children, ...props }: any) {
    return <a href={href} {...props}>{children}</a>;
  };
});

jest.mock("next/navigation", () => ({
  usePathname: jest.fn(),
}));

import { WorkspaceNav } from "./workspace-nav";
import { usePathname } from "next/navigation";

const mockUsePathname = usePathname as jest.MockedFunction<typeof usePathname>;

describe("WorkspaceNav", () => {
  test("renders all workspace links", () => {
    mockUsePathname.mockReturnValue("/hire");
    render(<WorkspaceNav />);
    expect(screen.getByText("Research")).toBeInTheDocument();
    expect(screen.getByText("Debate")).toBeInTheDocument();
    expect(screen.getByText("Explore")).toBeInTheDocument();
    expect(screen.getByText("Deploy")).toBeInTheDocument();
  });

  test("highlights active link", () => {
    mockUsePathname.mockReturnValue("/debate");
    render(<WorkspaceNav />);
    const debateLink = screen.getByText("Debate").closest("a");
    expect(debateLink?.style.color).toContain("var(--text)");
    expect(debateLink?.style.background).toContain("var(--surface2)");
  });

  test("non-active links use muted color", () => {
    mockUsePathname.mockReturnValue("/debate");
    render(<WorkspaceNav />);
    const researchLink = screen.getByText("Research").closest("a");
    expect(researchLink?.style.color).toContain("var(--muted)");
  });

  test("links point to correct paths", () => {
    mockUsePathname.mockReturnValue("/hire");
    render(<WorkspaceNav />);
    expect(screen.getByText("Research").closest("a")).toHaveAttribute("href", "/hire");
    expect(screen.getByText("Debate").closest("a")).toHaveAttribute("href", "/debate");
    expect(screen.getByText("Explore").closest("a")).toHaveAttribute("href", "/explore");
    expect(screen.getByText("Deploy").closest("a")).toHaveAttribute("href", "/deploy");
  });
});
