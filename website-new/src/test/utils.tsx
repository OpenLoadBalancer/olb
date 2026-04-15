import { render } from "@testing-library/react";

// Wrapper that provides any needed context (theme, etc.)
export function renderWithProviders(ui: React.ReactElement) {
  return render(ui);
}
