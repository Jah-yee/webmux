<script lang="ts">
  import { Badge, type BadgeVariant } from "$lib/components/ui/badge";
  import type { TunnelInfo } from "../schemas";

  let { state }: { state: TunnelInfo["state"] } = $props();
  const labels = {
    off: "未启用",
    starting: "启动中",
    ready: "已就绪",
    failed: "异常停止",
  };
  const variants: Record<TunnelInfo["state"], BadgeVariant> = {
    off: "outline",
    starting: "warning",
    ready: "success",
    failed: "destructive",
  };
</script>

<Badge variant={variants[state]}>{labels[state]}</Badge>
