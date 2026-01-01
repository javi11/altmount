#!/bin/bash

#############################################
# CONFIGURAÇÃO
#############################################

WATCH_DIR="/mnt/nvme15n1/orezraey/usenet/sync_nzbs"

API_KEY="API_KEY_HERE"
API_URL="http://127.0.0.1:64002/api/import/file"

MAX_RETRIES=2
STABLE_SECONDS=1
CHECK_INTERVAL=1
POST_SUCCESS_DELAY=5   # delay antes de remover NZB (importado com sucesso), em segundos [15 segundos]
EMPTY_DIR_AGE=1          # minutos

# arquivos em delay de remoção
declare -A REMOVAL_DELAY_FILES

# Função para obter caminho do lock
get_lock_path() {
    local nzb="$1"
    echo "${nzb}.lock"
}

# Função para limpar locks órfãos (locks sem NZB correspondente)
cleanup_orphan_locks() {
    find "$WATCH_DIR" -type f -name "*.lock" ! -path "*/.stfolder/*" | while read -r lock_file; do
        nzb_file="${lock_file%.lock}"
        if [[ ! -f "$nzb_file" ]]; then
            rm -f "$lock_file"
            log "✓ Removido lock órfão: $lock_file"
        fi
    done
}

#############################################
# FUNÇÕES
#############################################

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

wait_until_ready() {
    local file="$1"
    local last_size=-1
    local stable_time=0

    while true; do
        [[ ! -f "$file" ]] && return 1

        size=$(stat -c%s "$file" 2>/dev/null || echo 0)

        # nunca processar 0 bytes
        if [[ "$size" -eq 0 ]]; then
            stable_time=0
        elif [[ "$size" -eq "$last_size" ]]; then
            stable_time=$((stable_time + CHECK_INTERVAL))
        else
            stable_time=0
        fi

        [[ "$stable_time" -ge "$STABLE_SECONDS" ]] && return 0

        last_size="$size"
        sleep "$CHECK_INTERVAL"
    done
}

schedule_remove() {
    local nzb="$1"
    local lock_file="$2"

    # marca arquivo em delay de remoção
    REMOVAL_DELAY_FILES["$nzb"]=1

    (
        sleep "$POST_SUCCESS_DELAY"
        if [[ -f "$nzb" ]]; then
            rm -f "$nzb" "$lock_file"
            log "✓ Removidos NZB e lock após delay: $nzb"
        else
            # se NZB já desapareceu, garante remoção do lock
            rm -f "$lock_file"
            log "✓ Removido lock (sem NZB): $lock_file"
        fi
        # Remove arquivo associado (mesmo nome sem .nzb)
        local base_file="${nzb%.nzb}"
        if [[ -f "$base_file" ]]; then
            rm -f "$base_file"
            log "✓ Removido arquivo associado: $base_file"
        fi
        unset REMOVAL_DELAY_FILES["$nzb"]
    ) &
}

process_nzb() {
    local nzb="$1"
    local lock_file=$(get_lock_path "$nzb")

    [[ ! -f "$nzb" ]] && return

    # Verifica se já está sendo processado
    if [[ -f "$lock_file" ]]; then
        return
    fi

    # Cria lock
    touch "$lock_file"

    log "Detectado NZB: $nzb"
    log "Aguardando arquivo estabilizar..."

    if ! wait_until_ready "$nzb"; then
        log "Arquivo desapareceu antes de estabilizar"
        rm -f "$lock_file"
        return
    fi

    for ((i=1; i<=MAX_RETRIES; i++)); do
        log "Tentativa $i de importação"

        HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
            -X POST "${API_URL}?apikey=${API_KEY}" \
            -H "Content-Type: application/json" \
            -d "{\"file_path\":\"${nzb}\",\"relative_path\":\"${WATCH_DIR}\"}")
		
		log "{\"file_path\":\"${nzb}\",\"relative_path\":\"${WATCH_DIR}\"}"
		
        if [[ "$HTTP_CODE" == "200" ]]; then
            log "✓ Importado com sucesso"
            schedule_remove "$nzb" "$lock_file"
            return
        fi

        log "✗ Falha HTTP $HTTP_CODE"
        sleep 1
    done

    log "✗ Falha após $MAX_RETRIES tentativas"
    rm -f "$nzb" "$lock_file"
    log "✓ Removidos NZB e lock: $nzb"
}



cleanup_empty_dirs() {
    # Limpa locks órfãos antes de remover pastas vazias
    cleanup_orphan_locks

    find "$WATCH_DIR" -type d -empty -mmin +$EMPTY_DIR_AGE ! -path "*/.stfolder*" -exec rmdir {} \; 2>/dev/null
}

#############################################
# INICIALIZAÇÃO
#############################################

# Limpa locks órfãos na inicialização
cleanup_orphan_locks

log "Iniciando watcher com scan periódico..."

while true; do
    # captura arquivos numa array
    readarray -t nzbs < <(find "$WATCH_DIR" -type f -name "*.nzb" ! -path "*/.stfolder/*")
    for FILE in "${nzbs[@]}"; do
        process_nzb "$FILE" &
    done

    # remove pastas vazias mais antigas que EMPTY_DIR_AGE minutos
    cleanup_empty_dirs

    sleep 1
done
