// Put together using bubblegum and ductape by Milenko @ TeamUIX
// This is a Go port of unxip by MaTiAz5
// Using references from Pixit by Voltaic
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type XIPHeader struct {
	Magic      [4]byte
	DataOffset uint32
	Files      uint16
	Names      uint16
	DataSize   uint32
}

type FileData struct {
	Offset    uint32
	Size      uint32
	Type      uint32
	Timestamp uint32
}

type FileName struct {
	DataIndex  uint16
	NameOffset uint16
}

func getFilename(data []byte, offset int) string {
	var str []byte
	for i := offset; i < len(data); i++ {
		if data[i] == 0 {
			break
		}
		str = append(str, data[i])
	}
	return string(str)
}
func extractArchive(archiveFile, extractFolder string) error {
	data, err := ioutil.ReadFile(archiveFile)
	if err != nil {
		return err
	}

	header := XIPHeader{}
	reader := bytes.NewReader(data)
	err = binary.Read(reader, binary.LittleEndian, &header)
	if err != nil {
		return err
	}

	if string(header.Magic[:]) != "XIP0" {
		return fmt.Errorf("invalid file format")
	}

	fileData := make([]FileData, header.Files)
	err = binary.Read(reader, binary.LittleEndian, &fileData)
	if err != nil {
		return err
	}

	fileNames := make([]FileName, header.Names)
	err = binary.Read(reader, binary.LittleEndian, &fileNames)
	if err != nil {
		return err
	}

	sort.Slice(fileNames, func(i, j int) bool {
		return fileNames[i].NameOffset < fileNames[j].NameOffset
	})

	filenameBlockOffset := int64(header.Names)*int64(binary.Size(FileName{})) + int64(header.Files)*int64(binary.Size(FileData{})) + int64(binary.Size(XIPHeader{}))
	filenameBlockSize := int(header.DataOffset) - int(filenameBlockOffset)
	filenameBlock := data[filenameBlockOffset : filenameBlockOffset+int64(filenameBlockSize)]

	err = os.MkdirAll(extractFolder, 0755)
	if err != nil {
		return err
	}

	blockSize := 4096
	buffer := make([]byte, blockSize)
	//Output information about the loaded XIP file.
	// Print header information
	fmt.Printf("File ID....: %s\n", header.Magic)
	fmt.Printf("File Name..: %s\n", archiveFile)
	fmt.Printf("Data Offset: 0x%.8X\n", header.DataOffset)
	fmt.Printf("Files......: %d (Including Possible Mesh Buffers) \n", header.Files)
	fmt.Printf("Data Size..: 0x%.8X (%d bytes)\n", header.DataSize, header.DataSize)
	// Define the file types within a XIP file.
	fileTypes := map[uint32]string{

		0: "Generic",
		1: "Mesh",
		2: "Texture",
		3: "Wave",
		4: "Mesh Reference",
		5: "Index Buffer",
		6: "Vertex Buffer",
	}
	for i, fd := range fileData {
		filename := getFilename(filenameBlock, int(fileNames[i].NameOffset))
		dstPath := filepath.Join(extractFolder, strings.ReplaceAll(filename, "\\", string(filepath.Separator)))
		if fd.Type == 5 || fd.Type == 6 || fd.Type == 4 { // Skipping buffer files, and Mesh files. Extraction of mesh's is broken.
			fmt.Println("Skipping file of", filename, "which is a", fileTypes[fd.Type])
			continue
		}

		fmt.Printf("Extracting file '%s' ...\n", filename)

		err = os.MkdirAll(filepath.Dir(dstPath), 0755)
		if err != nil {
			return err
		}

		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		srcReader := bytes.NewReader(data[header.DataOffset+fd.Offset : header.DataOffset+fd.Offset+fd.Size])

		for {
			nRead, err := srcReader.Read(buffer)
			if err != nil && err != io.EOF {
				return err
			}
			if nRead == 0 {
				break
			}

			nWritten, err := dstFile.Write(buffer[:nRead])
			if err != nil {
				return err
			}
			if nRead != nWritten {
				return fmt.Errorf("error writing file")
			}
		}
	}

	return nil
}

func main() {

	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <archive>\n", os.Args[0])
		os.Exit(1)
	}

	// Take a filename as input and extract it to an output folder of the same name.

	archiveFile := os.Args[1]
	// output folder is the same name as the archive file, without the extension, inside output directory.
	extractFolder := "output/" + strings.TrimSuffix(archiveFile, filepath.Ext(archiveFile))
	err := extractArchive(archiveFile, extractFolder)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Extraction complete.")
}
