package main

import (
	"encoding/json"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"strconv"
	"time"
	// "k8s.io/client-go/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

// PodMetricsList : PodMetricsList
type PodMetricsList struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		SelfLink string `json:"selfLink"`
	} `json:"metadata"`
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			SelfLink          string    `json:"selfLink"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Timestamp  time.Time `json:"timestamp"`
		Window     string    `json:"window"`
		Containers []struct {
			Name  string `json:"name"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}


type PodDetails struct {
	Name         string
	Labels       map[string]string
	Namespace    string
	NodeName     string
	ActualCPU    float64
	RequestedCPU float64
}


type NodeDetails struct {
	Name string
	Cost float64
	Cores float64
}

func getMetrics(clientset *kubernetes.Clientset, nodeDetails map[string]NodeDetails ) (map[string]PodDetails, map[string][]PodDetails, map[string][]PodDetails, error) {
	var pods PodMetricsList

	podLookUp := make(map[string]PodDetails)
	appLookUp := make(map[string][]PodDetails)
	nodeLookUp := make(map[string][]PodDetails)

	data, err := clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/pods").DoRaw()
	if err != nil {
		return podLookUp, appLookUp, nodeLookUp, err
	}
	// fmt.Println( string(data))
	err = json.Unmarshal(data, &pods)
	for _, v := range pods.Items {
		pd, err := getPods(clientset, v.Metadata.Name, v.Metadata.Namespace)
		if err != nil {
			fmt.Printf("Error retrieving %v in %v \t\t %v \n", v.Metadata.Name, v.Metadata.Namespace, err)
		} else {
			acpu := 0.0

			for _, vv := range v.Containers {
				if len(vv.Usage.CPU) > 1 {
					vv.Usage.CPU = vv.Usage.CPU[:len(vv.Usage.CPU)-1]
				}
				i, err := strconv.Atoi(vv.Usage.CPU)
				if err != nil {
					fmt.Printf("ERROR ActualCPU \t\t %v \t\t %v\t\t %v\n", v)
				}
				acpu = acpu + float64(i)
			}

			//
			cores := nodeDetails[pd.NodeName].Cores
			//Get price e.g. 10 dollar per hour per core on an 8 core vm
			price := nodeDetails[pd.NodeName].Cost / cores
			pd.ActualCPU = acpu / 1000000000  * price
			pd.RequestedCPU = pd.RequestedCPU * price
			podLookUp[v.Metadata.Name] = pd
			if len(nodeLookUp[pd.NodeName]) < 1 {
				nodeLookUp[pd.NodeName] = make([]PodDetails, 0)
			}
			nodeLookUp[pd.NodeName] = append(nodeLookUp[pd.NodeName], pd)
			if pd.Labels["app"] != "" {
				if len(appLookUp[pd.Labels["app"]]) < 1 {
					appLookUp[pd.Labels["app"]] = make([]PodDetails, 0)
				}
				appLookUp[pd.Labels["app"]] = append(appLookUp[pd.Labels["app"]], pd)
			}
		}
	}
	return podLookUp, appLookUp, nodeLookUp, err
}
func getPods(clientset *kubernetes.Clientset, name string, namespace string) (PodDetails, error) {

	pod, err := clientset.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return PodDetails{}, err
	}
	rcpu := 0.0
	for _, v := range pod.Spec.Containers {

		q := v.Resources.Requests["cpu"]
		r := q.MilliValue()
		j := float64(r)/1000.0
		if err != nil {
			fmt.Printf("ERROR RequestCPU \t\t %v \n", v)
		}
		rcpu = rcpu + float64(j)
	}
	return PodDetails{
		Name:         pod.Name,
		Namespace:    pod.Namespace,
		NodeName:     pod.Spec.NodeName,
		Labels:       pod.Labels,
		RequestedCPU: rcpu,
	}, err

}

func getNodeDetails(clientset *kubernetes.Clientset) map[string]NodeDetails {
	toReturn := make(map[string]NodeDetails)
	nodesData, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})

	if err != nil {
		fmt.Printf("ERROR Getting Nodes \t\t %v \n", err)
	}
	// var nodes NodeList
	// err = json.Unmarshal(nodesData, &nodes)

	for _,v := range nodesData.Items {
		cpu := v.Status.Allocatable["cpu"]
		cpuInt,_ :=cpu.AsInt64()
		cpuFloat := float64(cpuInt)
		cost :=  cpuFloat
		if v.Labels["cost"] != "" {
			cost,err = strconv.ParseFloat(v.Labels["cost"],64)
			if err != nil {
				fmt.Printf("Error retrieving price, setting price to the no of cores", err)
			}
		}
		toReturn[v.Name] = NodeDetails{
			Name: v.Name,
			Cost: cost,
			Cores: cpuFloat,
		}
	}
	return toReturn
}

func main() {

	kubeconfig := "/Users/chris/.kube/config"
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	writePodData(clientset)
	nodeDetails := getNodeDetails(clientset)
	podLookUp, appLookUp, nodeLookUp, err := getMetrics(clientset,nodeDetails)
	if err != nil {
		panic(err.Error())
	}
	for _, m := range podLookUp {
		fmt.Printf("%v \t\t %v \t\t %v \t\t %v \n", m.ActualCPU, m.RequestedCPU, m.Name, m.Namespace)
	}
	for name, app := range appLookUp {
		acpu := 0.0
		rcpu := 0.0
		for _, pod := range app {
			acpu = acpu + pod.ActualCPU
			rcpu = rcpu + pod.RequestedCPU
		}
		fmt.Printf("%v\t\t%v\t\t%v\n", name, acpu, rcpu)
	}
	for name, node := range nodeLookUp {
		acpu := 0.0
		rcpu := 0.0
		for _, pod := range node {
			acpu = acpu + pod.ActualCPU
			rcpu = rcpu + pod.RequestedCPU
		}
		fmt.Printf("%v\t\t%v\t\t%v\n", name, acpu, rcpu)
	}
}
func writePodData(clientset *kubernetes.Clientset) {
	_, err := clientset.RESTClient().Get().AbsPath("apis/apiextensions.k8s.io/v1beta1/podscost.cminion.com").DoRaw()
	if err != nil {
		panic(err.Error())
	}
}
