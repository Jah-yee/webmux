import type { Meta, StoryObj } from "@storybook/svelte-vite";
import CodeNavigator from "./CodeNavigator.svelte";

const meta = {
  title: "Workspace/File explorer",
  component: CodeNavigator,
} satisfies Meta<typeof CodeNavigator>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Browse: Story = {};
