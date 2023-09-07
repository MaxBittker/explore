package main

import (
	"encoding/csv"
	"os"
	"strconv"

	"gonum.org/v1/gonum/mat"
)

func readCSV(filename string) *mat.Dense {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	rows := len(records)
	cols := len(records[0])
	dataRaw := make([]float64, 0, rows*cols)

	for _, row := range records {
		for _, val := range row {
			num, err := strconv.ParseFloat(val, 64)
			if err != nil {
				log.Fatal(err)
			}
			dataRaw = append(dataRaw, num)
		}
	}

	return mat.NewDense(rows, cols, dataRaw)
}
