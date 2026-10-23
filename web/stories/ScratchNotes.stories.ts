import type { Meta, StoryObj } from "@storybook/svelte-vite";
import ScratchNotes from "./ScratchNotes.svelte";

const meta = {
  title: "Workspace/Scratch notes",
  component: ScratchNotes,
} satisfies Meta<typeof ScratchNotes>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Empty: Story = { args: { count: 0 } };
export const Full: Story = { args: { count: 20 } };
export const Pending: Story = { args: { pending: true } };
export const MemoryOnly: Story = { args: { saved: "仅保留在当前页面" } };
