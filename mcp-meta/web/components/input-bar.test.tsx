import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { InputBar } from "./input-bar";

describe("InputBar", () => {
  test("renders with placeholder", () => {
    render(<InputBar onSubmit={jest.fn()} placeholder="Ask something..." />);
    expect(screen.getByPlaceholderText("Ask something...")).toBeInTheDocument();
  });

  test("renders default placeholder when none given", () => {
    render(<InputBar onSubmit={jest.fn()} />);
    expect(screen.getByPlaceholderText("What do you need?")).toBeInTheDocument();
  });

  test("submit button is disabled when input is empty", () => {
    render(<InputBar onSubmit={jest.fn()} />);
    const button = screen.getByRole("button", { name: "\u2191" });
    expect(button).toBeDisabled();
  });

  test("submit button is enabled when input has text", async () => {
    render(<InputBar onSubmit={jest.fn()} />);
    const input = screen.getByRole("textbox");
    await userEvent.type(input, "Hello");
    expect(screen.getByRole("button", { name: "\u2191" })).not.toBeDisabled();
  });

  test("calls onSubmit with trimmed text on form submit", async () => {
    const onSubmit = jest.fn();
    render(<InputBar onSubmit={onSubmit} />);

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "  Research AI  ");
    await userEvent.click(screen.getByRole("button", { name: "\u2191" }));

    expect(onSubmit).toHaveBeenCalledWith("Research AI");
  });

  test("clears input after submit", async () => {
    render(<InputBar onSubmit={jest.fn()} />);

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "Hello");
    await userEvent.click(screen.getByRole("button", { name: "\u2191" }));

    expect(input).toHaveValue("");
  });

  test("does not call onSubmit when input is whitespace only", async () => {
    const onSubmit = jest.fn();
    render(<InputBar onSubmit={onSubmit} />);

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "   ");
    await userEvent.click(screen.getByRole("button", { name: "\u2191" }));

    expect(onSubmit).not.toHaveBeenCalled();
  });

  test("renders with defaultValue", () => {
    render(<InputBar onSubmit={jest.fn()} defaultValue="prefilled" />);
    expect(screen.getByRole("textbox")).toHaveValue("prefilled");
  });

  test("submits on enter key", async () => {
    const onSubmit = jest.fn();
    render(<InputBar onSubmit={onSubmit} />);

    const input = screen.getByRole("textbox");
    await userEvent.type(input, "Hello{enter}");

    expect(onSubmit).toHaveBeenCalledWith("Hello");
  });
});
