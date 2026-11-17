package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

// As entradas de um índice são bytes comparados com bytes.Compare, sem saber
// os tipos das colunas. Para isso, a chave é codificada de um jeito em que a
// ordem dos bytes é a ordem dos valores:
//
//   - NULL vira 0x00, e um valor que não é NULL começa com 0x01. Assim NULL
//     vem antes de tudo.
//   - INT vira 8 bytes big endian com o bit de sinal invertido: os negativos
//     vêm antes dos positivos, e -1 vem logo antes de 0.
//   - REAL vira os 8 bytes do float64 (IEEE 754) em big endian. Num positivo,
//     o bit de sinal é ligado; num negativo, todos os bits são invertidos,
//     porque no IEEE 754 um negativo maior em módulo tem bits maiores.
//   - BOOLEAN vira 0x00 (FALSE) ou 0x01 (TRUE), como no PostgreSQL.
//   - TEXT vira os bytes do texto com cada 0x00 trocado por 0x00 0xFF, e
//     termina em 0x00 0x01. O terminador é menor que qualquer continuação,
//     então "a" < "ab", e uma chave de várias colunas não mistura o fim de
//     uma com o começo da outra: ("a", "bc") ≠ ("ab", "c").
//
// Prefixar o tamanho do texto, como no registro, não serviria: "b" (tamanho
// 1) viria antes de "aa" (tamanho 2).
//
// Cada entrada é a chave seguida do RID da linha (6 bytes big endian). Com
// isso, toda entrada é única, mesmo num índice que aceita chaves repetidas, e
// apagar a entrada de uma linha acha exatamente ela.

const ridSize = 6

// appendKey acrescenta a codificação de um valor a dst.
func appendKey(dst []byte, typ string, v Value) ([]byte, error) {
	if v.Null {
		return append(dst, 0x00), nil
	}
	dst = append(dst, 0x01)
	switch typ {
	case "INT":
		n, err := strconv.ParseInt(v.Text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q não é um INT", v.Text)
		}
		return binary.BigEndian.AppendUint64(dst, uint64(n)^(1<<63)), nil
	case "REAL":
		f, err := strconv.ParseFloat(v.Text, 64)
		if err != nil {
			return nil, fmt.Errorf("%q não é um REAL", v.Text)
		}
		bits := math.Float64bits(f)
		if bits&(1<<63) != 0 {
			bits = ^bits // negativo: inverte tudo, para -2 vir antes de -1
		} else {
			bits |= 1 << 63 // positivo: liga o bit de sinal, para vir depois dos negativos
		}
		return binary.BigEndian.AppendUint64(dst, bits), nil
	case "BOOLEAN":
		switch v.Text {
		case "FALSE":
			return append(dst, 0x00), nil
		case "TRUE":
			return append(dst, 0x01), nil
		}
		return nil, fmt.Errorf("%q não é um BOOLEAN", v.Text)
	case "TEXT":
		for _, b := range []byte(v.Text) {
			dst = append(dst, b)
			if b == 0x00 {
				dst = append(dst, 0xFF)
			}
		}
		return append(dst, 0x00, 0x01), nil
	}
	return nil, fmt.Errorf("tipo %q desconhecido", typ)
}

// encodeKey codifica os valores das colunas de um índice, em ordem.
func encodeKey(types []string, values []Value) ([]byte, error) {
	var key []byte
	for i, v := range values {
		var err error
		if key, err = appendKey(key, types[i], v); err != nil {
			return nil, err
		}
	}
	return key, nil
}

func appendRID(dst []byte, rid RID) []byte {
	dst = binary.BigEndian.AppendUint32(dst, rid.Page)
	return binary.BigEndian.AppendUint16(dst, rid.Slot)
}

// entryRID tira o RID do fim de uma entrada de índice.
func entryRID(entry []byte) RID {
	tail := entry[len(entry)-ridSize:]
	return RID{binary.BigEndian.Uint32(tail), binary.BigEndian.Uint16(tail[4:])}
}

// hasKey diz se a entrada começa com a chave key. Como cada valor codificado
// sabe onde termina, isso é o mesmo que "as primeiras colunas da entrada são
// iguais a key".
func hasKey(entry, key []byte) bool {
	return bytes.HasPrefix(entry, key)
}
