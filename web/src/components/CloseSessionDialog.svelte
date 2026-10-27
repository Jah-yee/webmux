<script lang="ts">
  import { errorMessage } from "../error";
  import { api } from "../api";
  import { NoContentSchema, type SessionInfo } from "../schemas";
  import { Button } from "$lib/components/ui/button";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";

  let {
    session,
    onclose,
    onremoved,
  }: {
    session: SessionInfo;
    onclose: () => void;
    onremoved?: () => void;
  } = $props();
  let open = $state(true);
  let busy = $state(false);
  let error = $state("");
  const removing = $derived(session.status === "exited");

  async function submit() {
    busy = true;
    error = "";
    try {
      await api(
        `/sessions/${session.id}${removing ? "?remove=1" : ""}`,
        NoContentSchema,
        "DELETE",
      );
      if (removing) onremoved?.();
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<AlertDialog.Root
  bind:open
  onOpenChange={(value) => {
    if (!value && !busy) onclose();
  }}
>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title
        >{removing ? "移除" : "关闭"}“{session.title}”？</AlertDialog.Title
      >
      <AlertDialog.Description>
        {removing
          ? "会话及其缓冲将永久移除。"
          : "正在运行的进程及子进程将终止。最后输出会保留，直到手动移除。"}
      </AlertDialog.Description>
    </AlertDialog.Header>
    {#if error}<p role="alert" class="text-destructive">{error}</p>{/if}
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>取消</AlertDialog.Cancel>
      <Button variant="destructive" disabled={busy} onclick={submit}>
        {busy ? "正在处理…" : removing ? "确认移除" : "确认关闭"}
      </Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
