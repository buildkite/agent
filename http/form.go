package http

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"reflect"
)

type Form struct {
	Params map[string]interface{}

	writer *multipart.Writer
}

func (f *Form) ToBody() (*bytes.Buffer, error) {
	body := &bytes.Buffer{}

	f.writer = multipart.NewWriter(body)

	for name, value := range f.Params {
		typeOf := reflect.TypeOf(value).String()

		if typeOf == "http.File" {
			file, _ := value.(File)

			part, err := f.writer.CreateFormFile(name, file.FileName)
			if err != nil {
				return nil, err
			}

			part.Write([]byte(file.Data))
		} else if typeOf == "int" {
			_ = f.writer.WriteField(name, fmt.Sprintf("%d", value))
		} else {
			_ = f.writer.WriteField(name, fmt.Sprintf("%s", value))
		}
	}

	err := f.writer.Close()
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (f *Form) ContentType() string {
	return f.writer.FormDataContentType()
}
