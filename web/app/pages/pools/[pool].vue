<template>
  <div>
    <div class="mb-2">
      <M3IconButton icon="mdi-arrow-left" :label="$t('pools.back')" to="/pools" />
    </div>

    <template v-if="pool">
      <PageHeader :title="pool.name" :subtitle="subtitle">
        <template #actions>
          <span class="text-label-medium text-on-surface-variant" data-test="live-status">
            {{ $t(`common.live.${status}`) }}
          </span>
          <M3Button
            :variant="pool.maintenance ? 'tonal' : 'outlined'"
            icon="mdi-wrench-outline"
            :disabled="busyMaintenance"
            data-test="maintenance-toggle"
            @click="maintenanceOpen = true"
          >
            {{ pool.maintenance ? $t('pools.maintenance.disable') : $t('pools.maintenance.enable') }}
          </M3Button>
        </template>
      </PageHeader>
      <div v-if="pool.maintenance" class="-mt-4 mb-6">
        <M3Chip tone="warning" icon="mdi-wrench-outline" small>{{ $t('pools.badges.maintenance') }}</M3Chip>
      </div>
    </template>

    <AppProblem :error="actionError ?? error" />

    <M3EmptyState
      v-if="!pool && !pending && isNotFound"
      :title="$t('pools.notFound')"
      icon="mdi-layers-outline"
    />

    <div v-if="pool" class="grid gap-6 lg:grid-cols-2">
      <PoolSpecView :spec="pool.spec" />

      <section :aria-labelledby="scalingHeadingId" class="rounded-md bg-surface-container-low p-4 shadow-elevation-1">
        <h2 :id="scalingHeadingId" class="mb-1 text-title-large text-on-surface">{{ $t('pools.scaling.heading') }}</h2>
        <p class="mb-4 text-body-medium text-on-surface-variant">{{ $t('pools.scaling.help') }}</p>
        <PoolScalingForm
          v-model="form"
          :mode="pool.spec.mode"
          :dirty="dirty"
          :busy="saving"
          @submit="askSave"
          @reset="form = baseline"
        />
      </section>
    </div>

    <section v-if="pool" :aria-labelledby="instancesHeadingId" class="mt-6">
      <h2 :id="instancesHeadingId" class="mb-4 text-title-large text-on-surface">{{ $t('pools.instances.heading') }}</h2>
      <PoolInstanceList :instances="instances" :now="now" />
    </section>

    <ConfirmDialog
      v-model="saveOpen"
      :title="$t('pools.confirm.saveTitle', { pool: pool?.name ?? '' })"
      :message="$t('pools.confirm.saveMessage')"
      :busy="saving"
      @confirm="saveScaling"
    />
    <ConfirmDialog
      v-model="maintenanceOpen"
      :title="maintenanceTitle"
      :message="maintenanceMessage"
      :busy="busyMaintenance"
      @confirm="toggleMaintenance"
    />
  </div>
</template>

<script setup lang="ts">
import { useId } from 'vue'
import { poolsApi } from '~/lib/api/pools'
import { createCoalescer, LIVE_REFRESH_DELAY_MS } from '~/lib/pools/coalesce'
import { applyScaling, rebaseForm, sameForm, toScalingForm } from '~/lib/pools/scaling'
import type { Scaling, ScalingForm } from '~/lib/pools/scaling'

// One pool: spec (read-only), scaling form, maintenance toggle and its instances. Live events re-run the load.
const route = useRoute()
const { t } = useI18n()
const snackbar = useSnackbar()
const { status, onResync, onEvent } = useGrusEvents()

const name = computed(() => String(route.params.pool))

const { data, error, pending, refresh } = await useAsyncData('pools.detail', async () => {
  const [poolData, instanceList] = await Promise.all([
    poolsApi.getPool(name.value),
    poolsApi.listPoolInstances(name.value),
  ])
  return { pool: poolData, instances: instanceList ?? [] }
})

const pool = computed(() => data.value?.pool)
// Checks the status field, not instanceof: an error restored from the SSR payload is a plain object.
const isNotFound = computed(() => (error.value as { status?: number } | null)?.status === 404)
const instances = computed(() => data.value?.instances ?? [])
const subtitle = computed(() => (pool.value ? [pool.value.spec.role, pool.value.spec.mode, pool.value.spec.driver].join(' · ') : ''))

// Reference time for the instance ages. Empty during SSR so the server and client markup agree.
const now = ref<number | null>(null)
function stamp() {
  now.value = import.meta.client ? Date.now() : null
}
onMounted(stamp)

// Scaling form: follows the server while the user has no unsaved edits, keeps their edits otherwise.
const baseline = ref<ScalingForm>(toScalingForm(data.value?.pool.spec.scaling ?? {}))
const form = ref<ScalingForm>({ ...baseline.value })
const dirty = computed(() => !sameForm(form.value, baseline.value))
watch(() => data.value?.pool.spec.scaling, (next) => {
  const fresh = toScalingForm(next ?? {})
  form.value = rebaseForm(form.value, baseline.value, fresh)
  baseline.value = fresh
  stamp()
})

// Mutations: every one asks first, then reloads the pool so the page shows what the server stored.
const saveOpen = ref(false)
const pendingScaling = ref<Scaling | null>(null)
const saving = ref(false)
const maintenanceOpen = ref(false)
const busyMaintenance = ref(false)
const actionError = ref<Error | null>(null)

const maintenanceTitle = computed(() => t(pool.value?.maintenance ? 'pools.maintenance.disableTitle' : 'pools.maintenance.enableTitle', { pool: name.value }))
const maintenanceMessage = computed(() => t(pool.value?.maintenance ? 'pools.maintenance.disableMessage' : 'pools.maintenance.enableMessage'))

function askSave(scaling: Scaling) {
  pendingScaling.value = scaling
  saveOpen.value = true
}

async function saveScaling() {
  const current = pool.value
  const scaling = pendingScaling.value
  if (!current || !scaling) return
  saving.value = true
  actionError.value = null
  try {
    await poolsApi.putPoolSpec(current.name, applyScaling(current.spec, scaling))
    snackbar.show(t('pools.saved'))
    await refresh()
  }
  catch (err) {
    actionError.value = err as Error
  }
  finally {
    saving.value = false
    pendingScaling.value = null
  }
}

async function toggleMaintenance() {
  const current = pool.value
  if (!current) return
  const enable = !current.maintenance
  busyMaintenance.value = true
  actionError.value = null
  try {
    await poolsApi.setMaintenance(current.name, enable)
    snackbar.show(t(enable ? 'pools.maintenanceOn' : 'pools.maintenanceOff'))
    await refresh()
  }
  catch (err) {
    actionError.value = err as Error
  }
  finally {
    busyMaintenance.value = false
  }
}

// Live updates: pool and instance events reload this pool, at most once per second.
const reload = createCoalescer(() => void refresh(), { delayMs: LIVE_REFRESH_DELAY_MS })
onEvent(['pool.*', 'instance.*'], () => reload.request())
onResync(() => reload.request())
onBeforeUnmount(() => reload.cancel())

const scalingHeadingId = `scaling-${useId()}`
const instancesHeadingId = `instances-${useId()}`
</script>
