<script lang="ts">
  import ResponsiveDialog from "../../src/components/ResponsiveDialog.svelte";

  let {
    busy = false,
    pinDrawerHeader = false,
    onclose,
  }: {
    busy?: boolean;
    pinDrawerHeader?: boolean;
    onclose: () => void;
  } = $props();
</script>

<ResponsiveDialog
  title="编辑工作区"
  description="桌面说明"
  drawerDescription="手机说明"
  {busy}
  {pinDrawerHeader}
  {onclose}
>
  <button type="button">表单操作</button>
</ResponsiveDialog>
