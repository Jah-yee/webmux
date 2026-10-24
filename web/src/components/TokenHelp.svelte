<script lang="ts">
  import * as Popover from "$lib/components/ui/popover";
</script>

<Popover.Root>
  <Popover.Trigger
    openOnHover
    openDelay={100}
    closeDelay={150}
    type="button"
    aria-label="关于共享 Token"
    class="inline-flex size-7 shrink-0 cursor-help items-center justify-center rounded-sm text-muted-foreground hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
  >
    <span
      aria-hidden="true"
      class="inline-flex size-4 items-center justify-center rounded-full border border-current text-xs font-semibold"
      >?</span
    >
  </Popover.Trigger>
  <Popover.Content
    role="dialog"
    aria-label="共享 Token 说明"
    align="start"
    collisionPadding={16}
    class="w-[min(20rem,calc(100vw-2rem))] space-y-2 leading-relaxed"
    trapFocus={false}
    onOpenAutoFocus={(event) => event.preventDefault()}
    onCloseAutoFocus={(event) => event.preventDefault()}
  >
    <p>Token 是这台 WebMux 的访问凭证，相当于共享密码。</p>
    <p>
      通过 <code>webmux config init</code>
      初始化配置时会自动生成，默认保存在服务所在机器的：
      <code class="block max-w-full break-all"
        >~/.config/webmux/config.toml</code
      >
      重启后继续使用。请查看其中的 <code>token</code> 值并填入输入框。
    </p>
    <p>
      用 <code>webmux config path</code> 查看默认路径；若启动时指定了
      <code>--config</code> 或 <code>--token</code>，以实际启动配置为准。
    </p>
  </Popover.Content>
</Popover.Root>
