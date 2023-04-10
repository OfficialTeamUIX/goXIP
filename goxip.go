// Put together using bubblegum and ductape by @dtoxmilenko
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
	"regexp"
	"sort"
	"strings"
)

// Constants for the XIP file types during creation.
const (
	MaxMeshBuffer           = 10    // Max amount of mesh buffers to create. (ib and vb files)
	MaxVertices             = 65536 // Max amount of vertices per mesh buffer.
	XIP_TYPE_GENERIC        = 0     // Generic Files
	XIP_TYPE_MESH           = 1     // XM Files, combined data from IB/VB/XIP_TYPE_MESH_REFERENCE.. This was confusing as hell.
	XIP_TYPE_TEXTURE        = 2     // XBX, BMP, TGA Files
	XIP_TYPE_WAVE           = 3     // WAV Files
	XIP_TYPE_MESH_REFERENCE = 4     // XM Files for sure.
	XIP_TYPE_INDEX_BUFFER   = 5     // IB Files
	XIP_TYPE_VERTEX_BUFFER  = 6     // VB Files
)

// XIP Header structure for creation.
type XIPHeader struct {
	Magic      [4]byte
	DataOffset uint32
	Files      uint16
	Names      uint16
	DataSize   uint32
}

// File Data structure for creation.
type FileData struct {
	Offset    uint32
	Size      uint32
	Type      uint32
	Timestamp uint32
}

// File Name structure for creation.
type FileName struct {
	DataIndex  uint16
	NameOffset uint16
}

// Mesh Buffer structure because meshes are a pain in the dick.
type MeshBuffer struct {
	FVF          uint32
	VertexStride int
	Vertices     []byte
	VertexCount  int
	Indices      []uint16
	IndexCount   int
}

var meshBuffers []MeshBuffer

var meshBuffersCreated bool = false
var vbAdded bool = false
var ibAdded bool = false

// Define the file types within a XIP file for infoXIP and extractXIP.
var xipFileTypes = map[uint32]string{
	0: "Generic",
	1: "Mesh",
	2: "Texture",
	3: "Wave",
	4: "Mesh Reference",
	5: "Index Buffer",
	6: "Vertex Buffer",
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

func compareNames(data []byte, fileNames []FileName, i, j int) bool {
	nameI := getFilename(data, int(fileNames[i].NameOffset))
	nameJ := getFilename(data, int(fileNames[j].NameOffset))
	return nameI < nameJ
}

func findMeshBuffer(fvf uint32, nVerts int) int {
	for i, meshBuffer := range meshBuffers {
		if meshBuffer.FVF == fvf && meshBuffer.VertexCount+nVerts <= MaxVertices {
			return i
		}
	}

	if len(meshBuffers) >= MaxMeshBuffer {
		fmt.Fprintf(os.Stderr, "Error: Too many mesh buffers.\n")
		os.Exit(1)
	}

	meshBuffers = append(meshBuffers, MeshBuffer{FVF: fvf})
	return len(meshBuffers) - 1
}

func addMesh(fileName string) (uint32, uint32, error) {
	meshFile, err := os.Open(fileName)
	if err != nil {
		return 0, 0, err
	}
	defer meshFile.Close()

	var primitiveType uint32
	var faceCount, fvf, vertexStride, vertexCount, indexCount uint32

	err = binary.Read(meshFile, binary.LittleEndian, &primitiveType)
	if err != nil {
		return 0, 0, err
	}
	err = binary.Read(meshFile, binary.LittleEndian, &faceCount)
	if err != nil {
		return 0, 0, err
	}
	err = binary.Read(meshFile, binary.LittleEndian, &fvf)
	if err != nil {
		return 0, 0, err
	}
	err = binary.Read(meshFile, binary.LittleEndian, &vertexStride)
	if err != nil {
		return 0, 0, err
	}
	err = binary.Read(meshFile, binary.LittleEndian, &vertexCount)
	if err != nil {
		return 0, 0, err
	}
	err = binary.Read(meshFile, binary.LittleEndian, &indexCount)
	if err != nil {
		return 0, 0, err
	}

	fvfIndex := findMeshBuffer(fvf, int(vertexCount))
	mb := &meshBuffers[fvfIndex]
	meshIndex := mb.IndexCount

	mb.VertexStride = int(vertexStride)

	vertices := make([]byte, (mb.VertexCount+int(vertexCount))*mb.VertexStride)
	copy(vertices, mb.Vertices)
	if _, err := meshFile.Read(vertices[mb.VertexCount*mb.VertexStride:]); err != nil {
		return 0, 0, err
	}
	mb.Vertices = vertices
	mb.VertexCount += int(vertexCount)

	indices := make([]uint16, mb.IndexCount+int(indexCount))
	copy(indices, mb.Indices)
	if err := binary.Read(meshFile, binary.LittleEndian, indices[mb.IndexCount:]); err != nil {
		return 0, 0, err
	}
	for i := 0; i < int(indexCount); i++ {
		indices[mb.IndexCount+i] += uint16(mb.VertexCount - int(vertexCount))
	}
	mb.Indices = indices
	mb.IndexCount += int(indexCount)

	meshID := uint32((fvfIndex << 24) | meshIndex)
	primCount := indexCount / 3

	return meshID, primCount, nil
}

func createMeshBuffers(outputDir string) error {
	// Print the output directory so we know where to look for the files.
	fmt.Printf("Output Directory: %s\n", outputDir)
	for i, mb := range meshBuffers {
		ibFileName := filepath.Join(outputDir, fmt.Sprintf("~%d.ib", i))
		ibFile, err := os.Create(ibFileName)
		if err != nil {
			return err
		}
		defer ibFile.Close()

		for _, index := range mb.Indices {
			if err := binary.Write(ibFile, binary.LittleEndian, index); err != nil {
				return err
			}
		}

		vbFileName := filepath.Join(outputDir, fmt.Sprintf("~%d.vb", i))
		vbFile, err := os.Create(vbFileName)
		if err != nil {
			return err
		}
		defer vbFile.Close()

		if err := binary.Write(vbFile, binary.LittleEndian, int32(mb.VertexCount)); err != nil {
			return err
		}
		if err := binary.Write(vbFile, binary.LittleEndian, mb.FVF); err != nil {
			return err
		}
		if _, err := vbFile.Write(mb.Vertices); err != nil {
			return err
		}
	}

	return nil
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
	// Output information about the loaded XIP file.
	// Print header information
	fmt.Printf("File ID....: %s\n", header.Magic)
	fmt.Printf("File Name..: %s\n", archiveFile)
	fmt.Printf("Data Offset: 0x%.8X\n", header.DataOffset)
	fmt.Printf("Files......: %d (Including Possible Mesh Buffers) \n", header.Files)
	fmt.Printf("Data Size..: 0x%.8X (%d bytes)\n", header.DataSize, header.DataSize)

	for i, fd := range fileData {
		filename := getFilename(filenameBlock, int(fileNames[i].NameOffset))
		dstPath := filepath.Join(extractFolder, strings.ReplaceAll(filename, "\\", string(filepath.Separator)))
		if fd.Type == 5 || fd.Type == 6 || fd.Type == 4 { // Skipping buffer files, and Mesh files. Extraction of mesh's is broken. But it appears to be working on creating XIP files.
			fmt.Println("Skipping file of", filename, "which is a", xipFileTypes[fd.Type])
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

// createXIP creates a XIP file from a folder.
func createXIP(folder, archiveFile string) error {
	var filePaths []string
	var fileData []FileData
	var fileNames []FileName
	var filenameBlock bytes.Buffer
	var fileContentBlock bytes.Buffer

	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		// Skip the root folder
		if path == folder {
			return nil
		}
		// Skip folders
		if info.IsDir() {
			return nil
		}

		if err != nil {
			return err
		}

		filePaths = append(filePaths, path)

		return nil
	})

	if err != nil {
		return err
	}

	for _, path := range filePaths {
		relPath, err := filepath.Rel(folder, path)
		if err != nil {
			return err
		}

		data, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		var fileType uint32
		ext := strings.ToLower(filepath.Ext(relPath))
		if ext == ".xm" {
			fileType = XIP_TYPE_MESH
		} else if ext == ".ib" {
			fileType = XIP_TYPE_INDEX_BUFFER
		} else if ext == ".vb" {
			fileType = XIP_TYPE_VERTEX_BUFFER
		} else if ext == ".xbx" {
			fileType = XIP_TYPE_TEXTURE
		} else if ext == ".wav" {
			fileType = XIP_TYPE_WAVE
		} else {
			fileType = XIP_TYPE_GENERIC
		}

		if fileType == XIP_TYPE_TEXTURE {
			textureFileName := strings.TrimSuffix(path, filepath.Ext(path))

			textureFileName += ".xbx"

			if _, err := os.Stat(textureFileName); err == nil {
				relPath, err = filepath.Rel(folder, textureFileName)
				if err != nil {
					return err
				}
			} else {
				fmt.Printf("WARNING: missing %s\n", textureFileName)
			}
		}
		// Create the index buffer and vertex buffer files on disk
		if err := createMeshBuffers(folder); err != nil {
			// Set bool to true or false depending on if the mesh buffers were created or not.
			meshBuffersCreated = false
			//return err
		} else {
			meshBuffersCreated = true
		}

		// Add the ib and vb files to the loop.

		// We find a mesh, we need to convert it to a mesh reference and its index buffer and vertex buffer. The Xbox Dashboard is fucking weird with textures and meshes.
		if fileType == XIP_TYPE_MESH {
			meshID, primCount, err := addMesh(path)
			if err != nil {
				return err
			}

			// Create a mesh reference entry in the file data
			meshRefEntry := FileData{
				Offset:    meshID,
				Size:      primCount,
				Type:      XIP_TYPE_MESH_REFERENCE,
				Timestamp: 0,
			}

			// Add the mesh reference to the file data
			fileData = append(fileData, meshRefEntry)

			// Create a file name entry for the mesh reference
			meshRefNameEntry := FileName{
				DataIndex:  uint16(len(fileData) - 1),
				NameOffset: uint16(filenameBlock.Len()),
			}

			// Add the file name entry to the file names slice
			fileNames = append(fileNames, meshRefNameEntry)

			// Get the name of the mesh reference without directory prefix
			meshRefName := filepath.Base(path)

			// Add the mesh reference name to the filename block
			filenameBlock.WriteString(strings.ReplaceAll(meshRefName, string(filepath.Separator), string(os.PathSeparator)))
			filenameBlock.WriteByte(0)
		}
		// Remove the original .xm file from the loop. This is what was fucking up the file structure.
		if fileType == XIP_TYPE_MESH {
			continue
		}

		// Sort the file data by the fileNames associate with the entry.
		sort.Slice(fileData, func(i, j int) bool {
			return fileNames[i].NameOffset < fileNames[j].NameOffset
		})

		// Make sure the offsets match up.
		if len(fileData) != len(fileNames) {
			return fmt.Errorf("fileData and fileNames are not the same length")
		}

		fileData = append(fileData, FileData{
			Offset:    uint32(fileContentBlock.Len()),
			Size:      uint32(len(data) + 1), // Add 1 to the size to account for the null byte
			Type:      fileType,
			Timestamp: 0,
		})

		// Add the filename to the filename list
		fileNames = append(fileNames, FileName{
			DataIndex:  uint16(len(fileData) - 1),
			NameOffset: uint16(filenameBlock.Len()),
		})

		// Write the filename to the filename block
		filenameBlock.WriteString(strings.ReplaceAll(relPath, string(filepath.Separator), string(os.PathSeparator)))
		filenameBlock.WriteByte(0)

		// Write the file content to the file content block
		fileContentBlock.Write(data)
		fileContentBlock.WriteByte(0)

		// check if vb file was added in relpath
		if strings.Contains(relPath, ".vb") {
			// if it was, set the vbAdded bool to true
			vbAdded = true
		}
		// check if ib file was added in relpath
		if strings.Contains(relPath, ".ib") {
			// if it was, set the ibAdded bool to true
			ibAdded = true
		}
		fmt.Printf("Adding file '%s' ...\n", relPath)
	}
	sort.Slice(fileNames, func(i, j int) bool {
		return strings.ToLower(filePaths[fileNames[i].DataIndex]) < strings.ToLower(filePaths[fileNames[j].DataIndex])
	})

	// Write the XIP header.
	headerSize := binary.Size(XIPHeader{})
	fileDataSize := len(fileData) * binary.Size(FileData{})
	fileNamesSize := len(fileNames) * binary.Size(FileName{})
	filenameBlockSize := len(filenameBlock.Bytes())
	fileContentBlockSize := len(fileContentBlock.Bytes())

	header := XIPHeader{
		Magic:      [4]byte{'X', 'I', 'P', '0'},
		DataOffset: uint32(headerSize + fileDataSize + fileNamesSize + filenameBlockSize),
		Files:      uint16(len(fileData)),
		Names:      uint16(len(fileNames)),
		DataSize:   uint32(fileContentBlockSize),
	}

	outputFileBuffer := &bytes.Buffer{}

	err = binary.Write(outputFileBuffer, binary.LittleEndian, header)
	if err != nil {
		return err
	}

	for _, fd := range fileData {
		err = binary.Write(outputFileBuffer, binary.LittleEndian, fd)
		if err != nil {
			return err
		}
	}

	for _, fn := range fileNames {
		err = binary.Write(outputFileBuffer, binary.LittleEndian, fn)
		if err != nil {
			return err
		}
	}

	_, err = outputFileBuffer.Write(filenameBlock.Bytes())
	if err != nil {
		return err
	}

	_, err = outputFileBuffer.Write(fileContentBlock.Bytes())
	if err != nil {
		return err
	}

	outputFile, err := os.Create(archiveFile)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	_, err = outputFile.Write(outputFileBuffer.Bytes())
	if err != nil {
		return err
	}

	return nil
}

func infoXIP(archiveFile string) error {
	// Open the XIP Archive
	archive, err := os.Open(archiveFile)
	if err != nil {
		return err
	}
	defer archive.Close()

	// Read the XIP Header
	var header XIPHeader
	err = binary.Read(archive, binary.LittleEndian, &header)
	if err != nil {
		return err
	}

	// Check the XIP Magic
	if header.Magic != [4]byte{'X', 'I', 'P', '0'} {
		return fmt.Errorf("invalid XIP magic")
	}

	// Print the XIP Header
	fmt.Printf("XIP Header:\n")
	fmt.Printf("  Magic: %s\n", string(header.Magic[:]))
	fmt.Printf("  Data Offset: %d\n", header.DataOffset)
	fmt.Printf("  Files: %d\n", header.Files)
	fmt.Printf("  Names: %d\n", header.Names)
	fmt.Printf("  Data Size: %d\n", header.DataSize)

	// Read the XIP File Data
	var fileData []FileData
	for i := 0; i < int(header.Files); i++ {
		var fd FileData
		err = binary.Read(archive, binary.LittleEndian, &fd)
		if err != nil {
			return err
		}
		fileData = append(fileData, fd)
	}

	// Read the XIP File Names
	var fileNames []FileName
	for i := 0; i < int(header.Names); i++ {
		var fn FileName
		err = binary.Read(archive, binary.LittleEndian, &fn)
		if err != nil {
			return err
		}
		fileNames = append(fileNames, fn)
	}

	// Print the filenames of the files within the XIP archive, and their assigned file types.
	fmt.Printf("XIP File Names:\n")
	for i := 0; i < int(header.Names); i++ {
		fn := fileNames[i]
		nameOffset := int64(fn.NameOffset)

		// Read the filename string from the archive
		filenameBlockOffset := int64(header.Names)*int64(binary.Size(FileName{})) + int64(header.Files)*int64(binary.Size(FileData{})) + int64(binary.Size(XIPHeader{}))
		archive.Seek(filenameBlockOffset+nameOffset, io.SeekStart)

		var nameBuf bytes.Buffer
		for {
			var b byte
			err = binary.Read(archive, binary.LittleEndian, &b)
			if err != nil {
				return err
			}
			if b == 0 {
				break
			}
			nameBuf.WriteByte(b)
		}
		name := nameBuf.String()

		fmt.Printf("  %s: %s\n", xipFileTypes[fileData[fn.DataIndex].Type], name)
	}

	return nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("Usage: goxip <command> <input>\n")
		fmt.Println("Commands:")
		fmt.Println("  extract: Extracts all files from an XIP archive into a folder.")
		fmt.Println("  create: Creates a new XIP archive from a folder.")
		fmt.Println("  info: Prints information about an XIP archive.")
		fmt.Println("Examples:")
		fmt.Println("  Extract a XIP archive: goxip extract sample.xip")
		fmt.Println("  Create a XIP archive: goxip create sample_folder")
		fmt.Println("  Print XIP archive info: goxip info sample.xip")
		os.Exit(1)
	}

	command := os.Args[1]
	input := os.Args[2]

	// Check for skin-related keywords in input
	match, _ := regexp.MatchString("(?i)skin(s?)", input)
	if match {
		fmt.Println("Warning: input contains skin-related keywords.")
		fmt.Println("This tool is currently incompatible with skin files.")
		fmt.Println("You can try to use them, but they won't work with UIX or UIX Lite.")
		// Wait for user input
		fmt.Println("Press enter to continue or q to quit.")
		var input string
		fmt.Scanln(&input)
		if input == "q" {
			os.Exit(1)
		}
	}

	switch command {
	case "extract":
		extractFolder := "output/" + strings.TrimSuffix(input, filepath.Ext(input))
		err := extractArchive(input, extractFolder)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Extraction complete.")

	case "info":
		err := infoXIP(input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

	case "create":
		// Create the output/newfolder directory if it doesn't exist
		err := os.MkdirAll("output/newfolder", 0755)
		if err != nil {
			fmt.Printf("Error creating directory: %v\n", err)
			os.Exit(1)
		}

		// Save the new archive inside the "output/newfolder" directory
		archiveFile := filepath.Join("output", "newfolder", filepath.Base(input)+".xip")
		err = createXIP(input, archiveFile)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		// Print a warning message if vb and ib files were not added
		if !vbAdded && !ibAdded && meshBuffersCreated {
			fmt.Println("Warning: vb and ib files were not added to the archive.")
			fmt.Println("Please re-run this tool.(Milenko cant fix this, because file loops are hard :D)")
			os.Exit(1)
		} else {
			fmt.Println("XIP archive created.")
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Printf("Usage: goxip <command> <input>\nCommands: extract, create\n")
		os.Exit(1)
	}
}
