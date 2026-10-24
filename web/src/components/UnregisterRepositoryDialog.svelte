<script lang="ts">
  import { Button } from "$lib/components/ui/button";
  import * as AlertDialog from "$lib/components/ui/alert-dialog";
  import { api } from "../api";
  import { NoContentSchema, type Repository } from "../schemas";

  let {
    repository,
    open = $bindable(),
    onchange,
  }: {
    // Kept after closing so the closing animation still shows its path.
    repository: Repository | null;
    open: boolean;
    onchange: () => Promise<void>;
  } = $props();

  let busy = $state(false);
  let error = $state("");
  $effect.pre(() => {
    // Each confirmation starts without the previous attempt's failure.
    if (open) error = "";
  });

  async function unregister() {
    if (!repository) return;
    busy = true;
    error = "";
    try {
      await api(`/repositories/${repository.id}`, NoContentSchema, "DELETE");
      open = false;
      await onchange();
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<AlertDialog.Root bind:open>
  <AlertDialog.Content>
    <AlertDialog.Header
      ><AlertDialog.Title>从列表移除仓库？</AlertDialog.Title
      ><AlertDialog.Description class="break-all whitespace-pre-wrap"
        >{repository?.anchorPath}<br
        />只移除持久登记，不删除任何目录或分支，不关闭会话。原会话仍可在「直接会话」中访问。</AlertDialog.Description
      ></AlertDialog.Header
    >
    {#if error}<p role="alert" class="text-sm break-all text-destructive">
        {error}
      </p>{/if}
    <AlertDialog.Footer
      ><AlertDialog.Cancel disabled={busy}>取消</AlertDialog.Cancel><Button
        disabled={busy}
        onclick={unregister}>{busy ? "正在移除…" : "确认移除登记"}</Button
      ></AlertDialog.Footer
    >
  </AlertDialog.Content>
</AlertDialog.Root>
